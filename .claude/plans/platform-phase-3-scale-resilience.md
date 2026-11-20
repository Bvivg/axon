# Фаза 3 — горизонтальное масштабирование и устойчивость

## Контекст
После фаз 1/1b/4 game уже многоинстансный (владельцы партиций, lease), а gateway, auth и chat
запускаются по одному. Rate limit живёт в памяти процесса gateway, поэтому при двух инстансах
лимит удваивается. Анонимные лимиты считаются по IP, и 100 пользователей за одним NAT делят
один бюджет. Fan-out чата между инстансами идёт через Redis pub/sub: сообщения, отправленные
во время обрыва соединения с Redis, этот инстанс не получит. Клиенты Connect работают без
таймаутов, ретраев и circuit breaker. При остановке сервис сразу закрывает сокеты, хотя
балансировщик ещё считает его живым.

Результат: overlay `docker-compose.ha.yml` поднимает по 2 инстанса gateway/auth/chat за Envoy;
overlay `docker-compose.chaos.yml` ставит Toxiproxy перед Postgres и Redis. Сценарии из
`core/deploy/chaos/` (задержка 300 мс до Postgres, обрыв Redis, рестарт gateway под нагрузкой)
проходят без потерянных ходов и сообщений.

## Решение
- **Rate limit — GCRA в Redis** (`gateway/internal/ratelimit/gcra.go`). Это один Lua-скрипт со
  временем Redis (`TIME`), поэтому часы инстансов не участвуют. Ключ авторизованного запроса —
  `user_id`. Для анонимного ключ `ip + X-Axon-Device`: web хранит случайный id устройства в
  localStorage. Отдельный лимитер `network` по одному IP (×10 к sensitive) не даёт обойти
  лимит сменой device id. Если Redis недоступен, лимитер переходит на локальный (таймаут
  50 мс) и считает отказы метрикой `gateway_rate_limit_store_errors_total`.
- **Отдельный `edge-redis`** — состояние границы (лимиты). Gateway не подключается к `data`,
  поэтому заводится зона `edge-state`, в которой только gateway и `edge-redis`.
- **Envoy** перед gateway в HA overlay: round-robin, health check `/readyz`, outlier detection,
  WebSocket upgrade. Envoy занимает host-порт gateway; у реплик gateway портов нет.
- **Fan-out чата — Redis Streams.** 16 шард-стримов `chat:fanout:<n>` (`XADD MAXLEN ~`),
  каждый инстанс читает все шарды одним `XREAD BLOCK`. После переподключения он продолжает
  с последнего прочитанного ID, а не с `$`, поэтому событие, отправленное во время обрыва,
  доставляется. Источник истины по-прежнему Postgres (seq сообщений).
- **Resilience** (`shared/pkg/resilience`): таймаут по умолчанию, ретраи только для
  идемпотентных RPC (`IdempotencyLevel`) с джиттером и бюджетом (10% от запросов, минимум 3/с),
  circuit breaker на процедуру. Подключается ко всем Connect-клиентам gateway. Список
  повторяемых read-RPC задаётся в gateway (`retryableProcedures`): смена `idempotency_level`
  в proto — ломающее изменение для `buf breaking` (категория FILE).
- **Graceful shutdown** во всех сервисах (`shared/pkg/lifecycle`): по сигналу `/readyz`
  отдаёт 503 `draining`, затем пауза `SHUTDOWN_DRAIN` (в HA overlay 5 с), чтобы Envoy снял
  инстанс, затем `Shutdown` HTTP (запросы в полёте дорабатывают), затем отмена стримов и
  сокетов, у game — сдача партий.
- **Лизы game — отдельный пул** (`GAME_LEASE_DATABASE_URL`, `GAME_LEASE_MAX_CONNS=4`, в HA
  overlay напрямую в Postgres мимо PgBouncer). `Claim`/`Renew`/`Release` и чтение таблицы
  владельцев не ждут в очереди общего пула за проектором и загрузкой партий.
- **Postgres**: streaming replica (`postgres-replica`, `pg_basebackup -R`) и PgBouncer в
  transaction mode. Сервисы ходят через PgBouncer; LISTEN/NOTIFY и миграции — напрямую.
  История партий (`ListHistory`) читается с реплики (`GAME_READ_DATABASE_URL`).
- **`game.moves` партиционирована по месяцам** (`played_at` = `change.At` события журнала,
  обрезанный до микросекунд, поэтому повтор журнала даёт те же строки; PK
  `(session_id, ply, played_at)`). Уникальность `(session_id, ply)` на партиционированной
  таблице не обеспечить, поэтому при отмене и повторе хода в одном пакете проектор удаляет
  старую строку. Партиции создаёт `game.ensure_move_partitions(months_ahead)` (миграция и
  game при старте и раз в сутки), строки из DEFAULT переносятся в новую партицию.
- **Toxiproxy** (`docker-compose.chaos.yml`, сценарии `core/deploy/chaos/*.sh`).

## Файлы
- `core/services/gateway/internal/{ratelimit,guard,config,cors}`, `cmd/gateway/main.go`.
- `ui/web/src/lib/connect/{device.ts,transport.ts}`.
- `core/shared/pkg/{resilience,health}`; main всех сервисов (drain, resilience).
- `core/services/chat/internal/pubsub/redis.go` — Streams вместо pub/sub.
- `core/services/game/migrations/000007_moves_partitioned.*`, `internal/projection/postgres.go`.
- `core/deploy/docker-compose.yml` (edge-redis), `docker-compose.ha.yml` (реплики, Envoy,
  replica, PgBouncer), `docker-compose.chaos.yml`, `core/deploy/{envoy,pgbouncer,chaos}/`.

## Как проверяется
- `go test -race ./...`, `make lint`, vitest web; miniredis-тесты GCRA и fan-out после обрыва.
- `docker compose ... -f docker-compose.ha.yml config -q`, то же с chaos.
- HA-стенд (`base + ha + chaos`, по 2 реплики gateway/auth/chat/game): нагрузчик 4000 партий
  (300 параллельно) прямо в реплики game; в это время задержка 300 мс до Postgres 20 с,
  обрыв Redis 10 с, рестарт game-1, chat-1, gateway-1, auth-1 по очереди. Сверка с read model:
  ни один подтверждённый ход не пропал, завершённые партии совпадают с БД ход в ход.
- E2E чата через Envoy в окне хаоса зелёные (кроме push, которому нужен VAPID-ключ).

## Критерий готовности
Рестарт любого одного контейнера под нагрузкой не роняет ни одной партии и сообщения.

## Результаты (2026-11-20)
Стенд `axon-ha-check` (`base + ha + chaos`): по 2 реплики gateway/auth/chat/game, 3 брокера
Kafka, Envoy, реплика Postgres, PgBouncer, Toxiproxy. Нагрузчик — 4000 партий трёх игр,
300 одновременно, паузы 260 мс; сценарий: задержка 300 мс до Postgres на 20 с → обрыв Redis
на 10 с → рестарт game-1 → chat-1 → gateway-1 → auth-1.

| Прогон | Доиграно и совпало с БД | Потеряно подтверждённых ходов | Сбои клиента |
|---|---|---|---|
| 1: game отменял запросы при SIGTERM, нагрузчик без переподключения | — | 0 | 1651 |
| 2: graceful shutdown game, нагрузчик переподключается | 3762 / 4000 | 0 | 238 таймаутов (ход, потерянный с сокетом, не переотправлялся) |
| 3: нагрузчик переотправляет ход | 3885 / 4000 | 0 | 115 × `4001 moved` |
| 4: лизы на отдельном пуле | 3999 / 4000 | 0 | 1 первое подключение во время рестарта game |

- Прогон 3 вскрыл флаппинг владения: под задержкой 300 мс общий пул game (10 соединений)
  держал продление лиз дольше бюджета 2 с, лизы истекали (`game_partition_revocations_total
  {reason="lease_expired"}` = 12), сокеты получали `4001` раз за разом, и длинные шахматные
  партии исчерпывали 20 переподключений. С отдельным пулом в прогоне 4 — 0 ошибок продления
  и только плановые `rebalance` при рестарте game-1.
- Передача партий: при остановке game-1 вторая реплика забрала 12 партиций за 0,3 с, при
  возвращении — ребаланс за 0,5 с.
- Рестарт gateway за Envoy: 0 ошибок соединения на upstream, 0 ретраев (drain 5 с).
- E2E чата через Envoy во время хаоса: всё зелёное, кроме push (нужен VAPID-ключ, которого
  нет в `.env.example`; падает и на базовом стенде).
- Нагрузка шла прямо в реплики game (тестовый вход `X-Axon-Test-User`); рестарт gateway
  проверен e2e чата и статистикой Envoy, а не нагрузчиком партий.
- `go test -race ./...` во всех модулях, `make lint` — 0 issues, `config -q` для base+ha,
  base+ha+chaos.
