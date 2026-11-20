# Фаза 1 — Game как stateful event-sourced сервис

Часть [platform-architecture-upgrade.md](platform-architecture-upgrade.md).

## Контекст

Сейчас каждая команда партии (ход, ничья, отмена, сдача, таймаут, вход второго игрока)
проходит через `session.Store.change`: `SELECT … FOR UPDATE` → изменение `Session` →
`UPDATE game.sessions` (+ `INSERT game.moves`) → `pg_notify`. Postgres — источник истины,
сервис без состояния, потолок ~1000 ходов/с при p50 152 мс (500 партий, ноутбук).

Цель фазы: источник истины — журнал Kafka, партия живёт в памяти владельца партиции,
Postgres — read model, который строит проектор. Падение владельца не теряет ходов.

## Решения

### Журнал
- Топик `game.session-events.v1`, 24 партиции. Dev-стек: 3 брокера KRaft, RF=3,
  `min.insync.replicas=2`. Web-e2e: 1 брокер, RF=1 (через env).
- Партиция = `fnv32a(session_id) % 24`, выставляется явно (manual partitioner), чтобы
  маршрутизатор и producer считали одинаково.
- Клиент — `github.com/twmb/franz-go`: идемпотентный producer, `acks=all`,
  колбэки assign/revoke у consumer group.
- Событие (`events.proto`, protobuf в значении записи): `session_id`, `seq` (= revision),
  `generation`, `at`, `actor`, факт (`Created`, `Joined`, `Moved{ply,data,state_before,clock_before}`,
  `DrawOffered`, `DrawDeclined`, `DrawAgreed`, `Resigned`, `TimedOut`, `Undone{ply}`,
  `Cancelled`) и `snapshot` — `Session` после события. Отдельная запись
  `PartitionClaimed{generation, owner}` — fencing-маркер.

### Домен
- Логика команд выносится из `Store` в чистые функции пакета `session`
  (`session.Decide*`): вход — `Session`, команда, `now`; выход — новая `Session` + факт
  или доменная ошибка. Тексты ошибок и коды наружу не меняются.
- Undo берёт предыдущее состояние из истории партии в памяти актора
  (подгружается из `game.moves.state_before` при активации).
- id партии = UUIDv5(`creator`, `request_id`) — повтор `CreateSession` идемпотентен
  без уникального индекса.

### Владение и акторы (`internal/ownership`, `internal/actor`)
- Consumer group `game-owners`: назначенные партиции = партии, которыми владеет инстанс.
- Assign(p): записать `PartitionClaimed{gen}` → дочитать журнал p от offset проектора
  до high watermark (наложить на read model) → поднять акторы всех незавершённых
  партий p → опубликовать владение в `game.owners` → начать принимать команды.
- Revoke(p): перестать принимать команды, дождаться in-flight записей, остановить акторы.
  Колбэк блокирует ребаланс, поэтому новый владелец стартует после сдачи.
- Актор: одна горутина на партию, очередь команд; команда → `Decide` → запись в журнал
  (синхронно, `acks=all`) → применение в памяти → ответ и уведомление подписчиков.
  Таймаут часов — таймер актора, событие `TimedOut`.

### Проектор (`internal/projector`)
- Consumer group `game-projector`, offset хранится в `game.projector_offsets` и пишется
  в той же транзакции, что и read model → exactly-once в Postgres.
- Применяет снимок к `game.sessions`, ведёт `game.moves`, по `Resigned/TimedOut/DrawAgreed/
  финальному Moved` у рейтинговой партии вызывает текущий `settle`.
- Fencing: событие с `generation` меньше последнего `PartitionClaimed` партиции отбрасывается.
- После коммита — `pg_notify(game_session_changed)`: сокеты подтягивают рейтинг.

### Маршрутизация
- `game.owners(partition, owner_id, address, generation)`; инстансы кэшируют, обновляют
  по `LISTEN game_owner_changed`.
- WS `/ws`: после первой команды `subscribe` не-владелец проксирует соединение владельцу.
  При смене владельца сокет закрывается кодом 4001, клиент переподключается (логика уже есть).
- Мутирующие RPC и `GetSession` форсятся владельцу внутренним Connect-вызовом с
  `X-Axon-Owner-Hop: 1` (защита от петли). `ListSessions`, рейтинги — из read model.

### Matchmaking
- Очередь остаётся в Postgres. Пара получает id партии и `started=false`; после
  `Start` у владельца флаг ставится в `true`, только тогда тикет показывает партию.
  Незапущенные пары перезапускаются фоновым проходом.

## Файлы
- `core/shared/proto/axon/game/v1/journal.proto` — `JournalRecord{SessionRecorded | PartitionClaimed}`, факты и снимок.
- `core/shared/proto/axon/game/v1/owner.proto` — внутренний `GameOwnerService.StartMatch` (gateway его не отдаёт).
- `core/services/game/internal/session/domain.go` — чистые `Rules.Create/Start/Join/Move/Draw/Undo/Leave/Expire`;
  `store.go` — только read model (List, рейтинги, очередь матчмейкинга, `Watch`).
- `core/services/game/internal/journal/` — `Log` (Kafka на franz-go и in-memory), кодек, `PartitionOf`, `Fence`.
- `core/services/game/internal/owner/` — партиции, акторы-ячейки с мьютексом, таймеры часов, claim/catch-up/revoke.
- `core/services/game/internal/projection/postgres.go` — проектор в `game.sessions/moves`, `settle`, offsets в той же транзакции.
- `core/services/game/internal/cluster/` — consumer group владельцев (lease-топик) и проектора.
- `core/services/game/internal/route/` — таблица владельцев по `LISTEN game_owner_changed`.
- `core/services/game/internal/server/` — RPC/WS через владельца, форвардинг (`forward.go`), relay WS, тестовый хук часов.
- `core/services/game/migrations/000005_event_log.up.sql` — `game.owners`, `game.projector_offsets`,
  `game.sessions.partition`, `game.match_queue.started`.
- `core/deploy/docker-compose.yml` — 3 брокера KRaft, `game` ×2; `docker-compose.web-e2e.yml` — 1 брокер, `game` ×2.

## Отклонения от замысла
- Актор — ячейка с мьютексом и таймером, а не горутина с очередью: проще, тот же порядок команд.
- Эпоха fencing берётся из `game.owners.generation` (Postgres), а не из generation consumer group:
  generation группы сбрасывается при удалении пустой группы.
- Владение раздаётся через отдельный lease-топик (`game.partition-leases.v1`), партиция lease = партиция журнала.
- e2e «server clocks…» двигал часы `UPDATE` в Postgres; теперь — журналируемый факт `ClockAdjusted`
  через `POST /internal/test/clock`, включается только `GAME_TEST_HOOKS=true`.
- Известное ограничение: зомби-владелец до истечения session timeout может дописать событие — его отбрасывают
  проектор и новый владелец, но клиент зомби успеет увидеть неподтверждённый ход.

## Проверка
- `go test -race ./...` в game: unit `Decide*` (перенос существующих сценариев),
  детерминированная симуляция актор+владение+проектор на in-memory журнале
  (случайные команды, падения владельца, дубликаты, зомби-generation) с инвариантами:
  нет потерянных и двойных ходов, `seq` монотонен, read model = свёртка журнала.
- Integration (testcontainers Kafka + Postgres): запись → проектор → read model, рестарт проектора.
- Web e2e chess/quoridor/tictactoe в Docker с двумя инстансами game — зелёные без правок тестов.
- Chaos: 50 партий, `docker kill` владельца посреди партий → все доиграны, число ходов
  в read model = числу подтверждённых.
- Нагрузка `load500`: 500 партий без пауз, сравнение с ~980 ходов/с, p50 152 мс.

## Критерий готовности
`docker kill` владельца посреди 50 партий не теряет ни одного подтверждённого хода, web e2e
зелёные на двух инстансах game, нагрузка измерена и записана здесь.

## Результат (20 ноября 2026)
- Unit: `go test -race` в game — симуляция хэндоверов (20 сидов × 400 шагов, 3 ноды), зомби-владелец,
  сбой записи в журнал, отмена ожидающей партии через хэндовер.
- Integration: проекция на Postgres (рейтинг начисляется один раз, ничья/мат/таймаут/сдача, fencing);
  testcontainers Kafka+Postgres — две ноды, остановка одной посреди 12 партий, рестарт проектора с откатом offset.
- Web e2e в Docker (1 брокер, game ×2): chess, quoridor, tic-tac-toe — 11 passed; env-gated тест часов
  зелёный на axon.test через `POST /internal/test/clock`.
- Chaos на axon.test (3 брокера, game ×2): 50 шахматных партий, `docker kill` владельца 12 партиций на 8-й секунде —
  все 50 доиграны, подтверждённых ходов 3937 = 3937 в read model, пауза на failover ~11 с (session timeout группы 10 с).
- Нагрузка 500 партий без пауз (те же партии, что в базовом замере):

  | | до (PG как источник истины) | после (журнал Kafka, game ×2) |
  |---|---|---|
  | время прогона | 41,4 с | 33,3 с |
  | применённых ходов/с | ~955 | ~1190 |
  | задержка хода p50 / p95 / p99 | 152 / 205 / 234 мс | 39 / 196 / 301 мс |
  | сбои | 0 | 0 |
  | пик CPU | game 2,2, postgres 1,9 ядра | game 1,7 + 1,5, kafka 1,5/1,3/1,3, postgres 1,3 ядра |

  Хвост p95/p99 — артефакт клиента: ответы стали быстрее лимита 4 команды/с на сокет, команды получают
  `RATE_LIMITED` и повторяются через 260 мс. Замер «после» шёл напрямую в gateway: TLS-фронт стенда
  (`calls-https`, node) на этой скорости сбрасывает часть из 1000 одновременных рукопожатий.

## A/B на тестовом входе (20 ноября 2026)
Нагрузчик `core/services/game/cmd/loadgen` ходит в game изнутри Docker-сети с `X-Axon-Test-User`
(`GAME_TEST_HOOKS=true`), минуя gateway и TLS-фронт. 10 000 пользователей вставлены в `auth.users`.
20 000 матчей поровну по трём играм, 300 одновременно, один seed, game ×2 в обеих схемах:

| | старая (Postgres — источник истины) | новая (журнал Kafka) |
|---|---|---|
| время | 850 с | 448 с |
| ходов/с | 803 | 1524 |
| задержка хода p50 / p95 / p99 | 277 / 767 / 1144 мс | 77 / 357 / 604 мс |
| сверка с read model | 683 639 = 683 639 | 683 639 = 683 639 |
| отставание read model после пика | 0 | 167 с |
| CPU, ядра (среднее) | game 2,7, Postgres 1,9 | game 4,5, Kafka 2,5, Postgres 0,6 |

Выигрыш ×1,9 по пропускной способности и ×3,6 по медиане. Узкое место переехало из Postgres в game:
каждое событие несёт полный снимок партии, producer не пакетирует записи, проектор пишет построчно.
Это и известное ограничение fencing — фаза 1b.
