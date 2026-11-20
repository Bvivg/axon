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
- `rating -rebuild` → пересчёт из топика совпадает с `rating.ratings` и лидербордами (exit 0).
- Три пункта выше одной командой: `make test-rating-e2e` (одноразовый `axon-rating-e2e`) или
  `core/deploy/rating-e2e.sh docker compose -p <проект> -f core/deploy/docker-compose.yml
  -f core/deploy/docker-compose.rating-e2e.yml` против поднятого стенда; ожидается exit 0.
- Browser e2e: `make test-web-e2e` — рейтинговые спеки chess/quoridor/tic-tac-toe зелёные.

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

## Решения по ходу работы (CDC)
- **Значение — `BinaryDataConverter`, а не `ByteArrayConverter`.** Payload outbox уходит сырыми
  байтами protobuf, но heartbeat-записи Debezium — `Struct`, и `ByteArrayConverter` на них
  падает. `BinaryDataConverter` пропускает `bytes` как есть, остальное отдаёт JSON-делегату.
- **Heartbeat через `pg_logical_emit_message`.** На PG17 pgoutput не шлёт пустые транзакции:
  пока в outbox никто не пишет, `confirmed_flush_lsn` слота стоит, а WAL копится (потолок —
  `max_slot_wal_keep_size=1GB`). `heartbeat.action.query` раз в 10 с пишет логическое сообщение,
  `message.prefix.exclude.list` не пускает его в топики.
- **Публикация `axon_outbox` с `publish='insert'`**: `Cleanup` удаляет доставленные строки, и
  эти DELETE не должны становиться событиями или tombstone.
- **Миграции — отдельные job'ы** `auth-migrate`, `chat-migrate`, `game-migrate`,
  `rating-migrate`: `cdc-setup` ждёт их все, потому что добавляет в публикацию только уже
  существующие `<schema>.outbox`, а сервисы ждут `cdc-setup`.
- **Пароль Debezium — через `EnvVarConfigProvider`** (`${env:DEBEZIUM_DB_PASSWORD}`): в
  REST-конфиге коннектора и в топике `_axon-cdc-configs` лежит плейсхолдер, а не пароль.
- **Apicurio in-memory**: после рестарта `registry` артефакт `axon.events.v1` пропадает
  (проверено: 200 → 404), пока не перезапущен `schema-register` (снова 200). Доставку это не
  задевает: ни конвертеры Debezium, ни консьюмеры registry не читают.

## Результаты (2026-11-20)
Стенд `axon-p2-check` — base compose, образы из ветки, все host-порты переопределены. Раннер —
`core/services/rating/e2e/chain_test.go` внутри стека (overlay `docker-compose.rating-e2e.yml`),
сценарий целиком — `core/deploy/rating-e2e.sh`; `make test-rating-e2e` гоняет его в одноразовом
`axon-rating-e2e`. Продуктовый код цепочки не понадобилось чинить ни разу.

**Одна рейтинговая партия через gateway** (два новых игрока, `FindMatch` + `/ws/game`,
крестики выигрывают за 5 полуходов):
- `game.outbox`: id — uuid v5 от сессии, `aggregatetype=game.finished.v1`,
  `aggregateid=tictactoe`, `type=axon.events.v1.GameFinished`, `rated=true`, игроки в порядке
  мест, `winner_seat=0`; строка есть через 8 мс после последнего хода.
- Kafka `game.finished.v1`: ключ `tictactoe`, заголовки `event_id` и `event_type` (плюс `id` —
  его EventRouter добавляет сам), value байт в байт равен payload из outbox.
- `rating.processed_events` → сессия; `rating.ratings`: 1362,31 и 1037,69, RD 290,32, σ 0,06;
  `rating.changes` — 2 строки. `rating.changed.v1`: ключ `tictactoe`, `1200→1362`, `1200→1038`.
- `game.ratings` = 1362/1038 и 2 строки `game.rating_changes` через 1,7–3,4 с после последнего
  хода; открытые сокеты обоих игроков получили сессию с `rating_changes` (±162) через 1,3–3,4 с
  после финальной доски. `GetRatings` через gateway — 1362 (1-0) и 1038 (0-1), `GetLeaderboard`
  у `rating` — победитель выше, 1362/1038.

**`connect` остановлен на 60 с под нагрузкой**: 10 игроков, раунды quick match раз в 7 с,
50 рейтинговых партий за 65 с (32 победы, 18 ничьих), 40 из них закончились при неактивном слоте
Debezium. После `start connect` коннектор снова RUNNING через 23 с, `rating.processed_events`
догнал все партии через 30 с после последней. Итог: в `processed_events` 50 сессий, каждая ровно
раз; `rating.changes` — 100 строк, `game.rating_changes` — 100 без дублей; `game.ratings` равен
`rating.ratings` (рейтинг и счётчики) у всех 10 игроков; в `game.finished.v1` 50 записей на
50 событий — graceful stop сохранил offset, повторной доставки не было.

**Rebuild** сразу после этого: `rating -rebuild` → exit 0 — 83 события из топика (вся история
стенда), 83 применено, 0 дублей, 26 игроков, совпадает с `rating.ratings` и
`leaderboard:tictactoe`, 130 мс.

**Browser e2e** (`make test-web-e2e`): в `docker-compose.web-e2e.yml` добавлены
`wal_level=logical`, `rating-migrate`, `cdc-setup`, `connect`, `cdc-connector`, `rating`;
Playwright стартует после регистрации коннектора. Спеки ждут Glicko-чисел с таймаутом 30 с:
tic-tac-toe `Rating +162`/`−162` и 1362 в лобби, quoridor `Resignation · Rating −162` и 1038,
chess draw `Rating ±0` и 1200. UI менять не пришлось: экран партии берёт дельту из повторного
пуша сокета. Итог: 76 passed, 1 skipped (часы на фикстурной БД), 7 мин. Первый прогон
параллельно с чужим стеком дал ещё три таймаута в чат-спеках (`long-message`, `media`,
`search`); отдельно и во втором полном прогоне они зелёные.

**Повтор с нуля**: `make test-rating-e2e` на свежем `axon-rating-e2e` — обе проверки зелёные,
50 партий (45 при остановленном `connect`), догон за 22 с, rebuild по 51 событию → exit 0.

**Проверки**: `go test -race -count=1 ./services/rating/...`, `make lint` — 0 issues; web
`lint` — 0 ошибок (3 старых предупреждения React Compiler), `typecheck` чистый, `vitest` —
124/125: `chess-bot` «mate in one» упирается в таймаут 5 с под нагрузкой, отдельно проходит
за 4,4 с; `config -q` для base, base+ha, base+observability, base+rating-e2e, web-e2e.

**По ходу**: `FindMatch` в tier sensitive (burst 5, 10 в минуту на игрока) — нагрузчик упёрся в
`resource_exhausted` на 7-м подряд раунде, теперь раннер выдерживает `Retry-After` и шаг раундов.
`auth.spec` ждал пустой `localStorage`, а с фазы 3 там лежит `axon.device` — спека теперь
пропускает только его и проверяет, что токенов там нет.

## Не сделано / открыто
- Партии, сыгранные до перехода, в `game.finished.v1` не попадают: `rating` стартует с нуля,
  старые Elo-значения в `game.ratings` перезаписываются при следующей партии игрока.
- `-apply` не публикует исправленные рейтинги в `rating.changed.v1`: `game.ratings` догоняет
  игрока на его следующей партии.
- Gateway/web для `RatingService/GetLeaderboard` не подключены.
- Лобби не подписано на рейтинг: если вернуться в него раньше, чем пришёл `rating.changed`
  (1–3 с), рейтинг обновится только при следующем запросе. Экран партии дельту ловит — сокет
  повторно присылает сессию с `rating_changes`.
