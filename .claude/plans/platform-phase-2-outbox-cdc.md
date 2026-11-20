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

## Решения по ходу работы (рейтинг)
- **Что публикует game.** `game.finished.v1` пишется при каждом переходе двухместной партии в
  «закончена» (флаг `rated` — то же правило, что было у `Settle`: `Rated` и есть соперник),
  а не только для рейтинговых; `rating` пропускает `rated=false`. Переход ловится в fold
  (база не закончена → закончена), так что повтор журнала с любого offset событие не добавляет.
  `event_id` = uuid v5 от `session_id`, `finished_at` — время события журнала, ключ — вид игры.
- **`outbox.Write` идемпотентен** (`ON CONFLICT (id) DO NOTHING`): пересборка read model из
  журнала даёт те же id и не роняет проектор. Дубли после `Cleanup` отсекает
  `rating.processed_events`.
- **Glicko-2 стартует с 1200/350/0.06**, а не 1500: формула инвариантна к сдвигу, а 1200 —
  прежнее значение по умолчанию в `GetRatings` и матчмейкинге. Один рейтинговый период на
  партию, RD ограничен 350, τ = 0.5.
- **Rebuild без теневых таблиц**: `/service -rebuild` читает топик с offset 0 одноразовым
  консьюмером без группы до end offsets, считает тем же `ledger.Book` в памяти и сравнивает с
  `rating.ratings` и `leaderboard:<game>` (код выхода 2 при расхождении). `-rebuild -apply`
  заменяет `ratings`, `changes`, `processed_events` и лидерборды; отказывает, если живой
  консьюмер уже обработал событие, которого rebuild не видел. Если топик обрезан retention'ом
  (start offset > 0), rebuild отказывает: `game.finished.v1` должен храниться бессрочно.
- **Game consumer** `game-ratings` на `rating.changed.v1`: upsert `game.ratings` абсолютными
  значениями (порядок по ключу-игре гарантирует партиция), `game.rating_changes` — `ON CONFLICT
  DO NOTHING` и только для существующей сессии, после вставки — `pg_notify`, чтобы клиенты
  увидели дельту. Elo-пакет удалён.
- **Роли Postgres на живом томе**: `01-schemas.sh` стал идемпотентным, job `postgres-schemas`
  прогоняет его на каждом `up` (и в `start-dev`), поэтому роль `rating_service` появляется без
  `make reset`.

## Не сделано / открыто
- Партии, сыгранные до перехода, в `game.finished.v1` не попадают: `rating` стартует с нуля,
  старые Elo-значения в `game.ratings` перезаписываются при следующей партии игрока.
- `-apply` не публикует исправленные рейтинги в `rating.changed.v1`: `game.ratings` догоняет
  игрока на его следующей партии.
- Web e2e (`chess`/`quoridor`/`tic-tac-toe.spec.ts`) ждут Elo-чисел (`Rating +20`, `1220`) и
  синхронного рейтинга; после подключения CDC в `docker-compose.web-e2e.yml` их нужно
  перевести на Glicko-2 (1200 → 1362/1038 за первую партию) и ожидание асинхронного обновления.
- Gateway/web для `RatingService/GetLeaderboard` не подключены.
