# Фаза 2 — Outbox + CDC и рейтинг как отдельный сервис

## Контекст
Рейтинг сейчас считает проектор game в той же транзакции, что и read model (`session.Settle`).
Пересчитать рейтинг с нуля или сменить формулу нельзя, не трогая game. Chat и auth не публикуют
доменные события вообще. Прямая запись в Kafka из обработчика — это dual-write: транзакция в
Postgres прошла, а publish упал (или наоборот).

Результат: chat, auth и game пишут события в свою таблицу `outbox` в той же транзакции, что и
изменение домена. Debezium (Kafka Connect) читает outbox через logical replication и раскладывает
события по топикам. Новый сервис `rating` считает Glicko-2 из `game.finished.v1` и может
пересобрать рейтинги и лидерборд с нуля из топика.

## Решение
- **Контракты событий** — `core/shared/proto/axon/events/v1/events.proto`: `GameFinished`,
  `ChatMessageSent`, `UserRegistered`, `CallEnded`, `RatingChanged`. В Kafka лежит
  protobuf-payload, тип — в заголовке `event_type`, id события — в заголовке `event_id`.
- **`shared/pkg/outbox`**: `Write(ctx, tx, table, Event)` — одна строка
  `(id, aggregatetype, aggregateid, type, payload, created_at)` в транзакции вызывающего;
  `Cleanup` удаляет строки старше суток (Debezium читает WAL, а не таблицу).
- **Таблицы outbox** — миграции `auth`, `chat`, `game`, `rating`. Публикация `axon_outbox` и роль
  для Debezium создаёт одноразовый job `cdc-setup` (суперпользователь), Postgres в base работает
  с `wal_level=logical`.
- **Debezium** (`debezium/connect`) с Outbox Event Router: топик = `aggregatetype`
  (`game.finished.v1`, `chat.message.v1`, `auth.user.registered.v1`, `call.ended.v1`,
  `rating.changed.v1`), ключ = `aggregateid`, payload без конвертации (ByteArray). Коннектор
  регистрирует job `cdc-connector` (PUT в REST API Connect, идемпотентно).
- **Schema Registry** — Apicurio (in-memory); job регистрирует `events.proto` как артефакт
  `axon.events.v1`. Совместимость схем проверяет `buf breaking` в CI.
- **Порядок для рейтинга.** Ключ `game.finished.v1` — вид игры, поэтому все партии одной игры
  идут в одну партицию в порядке коммита. Рейтинги считаются по виду игры, значит повтор топика
  даёт тот же порядок и те же числа.
- **Сервис `rating`** (`core/services/rating`): consumer group `rating` на `game.finished.v1`,
  Glicko-2 (один рейтинговый период на партию), схема `rating`: `ratings`, `changes`,
  `processed_events` (id события — первичный ключ, дубли пропускаются в той же транзакции).
  Изменения рейтинга уходят в свой outbox как `rating.changed.v1`. Лидерборд — Redis sorted
  sets `leaderboard:<game>`, обновляются после коммита. Режим `-rebuild` читает топик с начала в
  чистые теневые таблицы и сравнивает с живыми.
- **Game**: проектор при завершении рейтинговой партии пишет `game.finished.v1` в `game.outbox`
  вместо `Settle`; consumer `rating.changed.v1` обновляет локальные `game.ratings` и
  `game.rating_changes` (матчмейкинг и `GetRatings` работают как раньше, но источник истины —
  `rating`).

## Файлы
- `core/shared/proto/axon/events/v1/events.proto`, `core/shared/pkg/outbox/`.
- Миграции outbox: `core/services/{auth,chat,game}/migrations/`, `core/services/rating/migrations/`.
- `core/services/rating/` — новый модуль (go.work, Dockerfile.service SERVICE=rating, compose).
- `core/services/game/internal/{projection,session}` — outbox вместо Settle, consumer рейтинга.
- `core/services/chat` (отправка сообщения, завершение звонка), `core/services/auth` (регистрация).
- `core/deploy/docker-compose.yml` — `wal_level=logical`, `connect`, `cdc-setup`,
  `cdc-connector`, `registry`, `rating`; `core/deploy/cdc/`.

## Как проверяется
- Unit: Glicko-2 (эталонный пример Glickman), outbox (строка в транзакции, откат — нет строки),
  идемпотентность consumer'а rating, game: финиш партии → строка outbox.
- Стенд: сыграть рейтинговые партии → `rating.ratings` = ожидаемое, `game.ratings` догнал.
- Остановить `connect` на минуту под нагрузкой → после старта все события доставлены,
  `processed_events` = числу рейтинговых партий, дублей нет.
- `rating -rebuild` → теневые таблицы и лидерборд совпадают с живыми.

## Критерий готовности
Рейтинг считается отдельным сервисом из событий, и его можно пересчитать с нуля.
