# Axon — максимальное архитектурное усиление

## Контекст

Axon — учебный портфолио-проект. Цель усиления — не реальная нагрузка, а
демонстрация архитектурных решений уровня распределённой системы, каждое из
которых доказано тестом или замером, а не нарисовано на схеме.

Исходная точка (2026-11-20, после PR #10–#12):
- сервисы без состояния, Postgres — источник истины, ход = транзакция
  `SELECT … FOR UPDATE` → `INSERT game.moves` → `UPDATE game.sessions` → `pg_notify`;
- потолок на ноутбуке ~1000 ходов/с, p50 152 мс при 500 партиях (замер
  `scratchpad/load500`), упор в game (2,2 ядра) и Postgres (1,9 ядра);
- Kafka есть в compose и `shared/pkg/kafka`, но игрой не используется;
- один инстанс каждого сервиса, один брокер, один Postgres, нет трейсинга и метрик-дашбордов.

Результат усиления: game становится stateful-сервисом с event sourcing на Kafka,
всё масштабируется горизонтально, падение любого узла посреди партии не теряет ходов,
и это видно в Grafana и подтверждено chaos-тестами.

## Принципы

- Каждое решение закрывается доказательством: тест, chaos-сценарий или замер до/после.
- Источник истины для ходов — журнал в Kafka; Postgres — read model (CQRS).
  Правило `infra.md` «Redis не источник истины» сохраняется.
- Контракты событий — protobuf из `shared/proto`, совместимость проверяется `buf breaking` в CI.
- Новые компоненты поднимаются тем же `docker compose`, потом переносятся в k8s.

---

## Фаза 1 — Game как stateful event-sourced сервис (ядро усиления)

**Решение.** Партия — актор (горутина с mailbox) в памяти инстанса game, владеющего
Kafka-партицией этой партии. Ход проверяется актором, дописывается в топик
`game.moves.v1` (ключ `session_id`, 24 партиции, `acks=all`, `min.insync.replicas=2`)
и только после подтверждения уходит игрокам. Postgres обновляет проектор.

- Kafka-кластер из 3 брокеров KRaft, RF=3.
- Владение партиями — consumer group `game-owners`: assignment = набор партий,
  которыми управляет инстанс. При ребалансе актор сдаёт партию: дописывает
  snapshot-маркер, закрывает сокеты кодом `4001 moved`, клиент переподключается.
- Восстановление: последний snapshot из `game.snapshots` (Postgres) + replay
  журнала с сохранённого offset. Snapshot — каждые 50 ходов и при сдаче партии.
- Идемпотентность: у команды хода `command_id`; актор хранит последние N в состоянии,
  повтор возвращает прежний результат. Producer — idempotent (`enable.idempotence`).
- Fencing: в каждой записи `owner_epoch`; проектор и новые владельцы отбрасывают
  записи от устаревшего epoch (защита от «зомби»-инстанса после partition).
- Проектор `game-projector` (отдельная consumer group) пишет `game.moves`,
  `game.sessions` батчами в одной транзакции вместе с offset — exactly-once в read model.
- Часы партии — в акторе; таймаут — событие в журнале, а не cron.

**Маршрутизация.** Gateway держит карту `partition → instance` (подписка на Redis-ключ,
который публикует лидер группы) и проксирует WS партии на владельца. `GetSession`
читает read model; для read-your-writes ответ на ход содержит `revision`, клиент
ждёт его в read model или берёт снимок из WS.

**Файлы.**
- `core/shared/proto/axon/game/v1/events.proto` — `MoveApplied`, `SessionStarted`,
  `SessionFinished`, `SnapshotTaken`, `OwnerEpoch`.
- `core/services/game/internal/actor/` — актор, mailbox, recovery, snapshot.
- `core/services/game/internal/ownership/` — consumer group, assignment, epoch, handoff.
- `core/services/game/internal/projector/` — проектор в Postgres.
- `core/services/game/migrations/000005_event_sourcing.up.sql` — `game.snapshots`,
  `game.projector_offsets`.
- `core/services/gateway/internal/wsproxy/` — маршрутизация по владельцу.
- `core/deploy/docker-compose.yml` — 3 брокера, 2 инстанса game, проектор.
- `session.Store.Move` остаётся только как путь проектора и для тестов совместимости.

**Проверка.**
- Deterministic simulation test актора (фиксированный seed: ходы, падения, ребаланс,
  дубликаты) — инварианты: нет потерянных/двойных ходов, ply монотонен.
- Chaos e2e: убить инстанс-владельца и один брокер посреди 50 партий → все партии
  доиграны, ходы в read model совпадают с журналом.
- Load: тот же сценарий 500 партий без пауз — сравнение с исходными ~1000 ходов/с, p50 152 мс.

**Готово когда:** kill -9 владельца посреди партии не теряет ни одного хода, а нагрузочный
тест показывает прирост пропускной способности против текущей схемы.

---

## Фаза 2 — Outbox + CDC для остальных доменов

**Решение.** Chat и auth публикуют доменные события через transactional outbox,
а Debezium (Kafka Connect) читает outbox через logical replication. Сервисы не
пишут в Kafka напрямую — нет dual-write.

- События: `chat.message.v1`, `auth.user.registered.v1`, `call.ended.v1`, плюс
  `game.finished.v1` из журнала игры.
- Schema Registry (Apicurio или Confluent) с protobuf-схемами из `shared/proto`.
- Потребители: `rating` (Glicko-2 по `game.finished`), `notify` (push),
  `leaderboard` (Redis sorted sets, перестраиваются replay-ем топика).

**Файлы.** `core/deploy/connect/` (конфиг Debezium), `core/services/rating/`,
миграции outbox в chat/auth, `core/shared/proto/axon/events/v1/`.

**Проверка.** Убить Connect на 1 минуту → после старта все события доставлены,
дублей в потребителях нет (идемпотентные ключи). Пересборка лидерборда с нуля = текущий.

**Готово когда:** рейтинг считается отдельным сервисом из событий, и его можно пересчитать с нуля.

---

## Фаза 3 — Горизонтальное масштабирование и устойчивость

- По 2+ инстанса gateway, auth, chat; L7-балансировщик (Envoy) перед gateway.
- Распределённый rate limit в Redis (GCRA), ключ `user_id` для авторизованных,
  `ip` + device fingerprint для логина — снимает проблему «100 пользователей за одним NAT».
- Fan-out realtime между инстансами gateway — Redis Streams (consumer groups, ack),
  а не pub/sub без гарантий.
- Postgres: primary + streaming replica (Patroni или CloudNativePG позже), PgBouncer
  в transaction mode, read model истории читается с реплики.
- `game.moves` партиционирована по месяцам.
- Таймауты, retry с бюджетом и circuit breaker на каждом gRPC-клиенте (`shared/pkg/resilience`).
- Graceful shutdown: drain сокетов, сдача партий, `/readyz` = false до выхода.

**Проверка.** Toxiproxy-сценарии в `core/deploy/chaos/`: задержка 300 мс до Postgres,
обрыв Redis, рестарт gateway — e2e партий и чата зелёные.

**Готово когда:** рестарт любого одного контейнера под нагрузкой не роняет ни одной партии и сообщения.

---

## Фаза 4 — Observability полного цикла

- OpenTelemetry SDK во всех сервисах; trace context в gRPC metadata, WS-кадрах и
  Kafka headers (`traceparent`) — один трейс от клика до проектора.
- Стек: OTel Collector → Tempo (трейсы), Prometheus (метрики, exemplars), Loki (логи),
  Grafana с дашбордами: ходы/с, p50/p99 хода, lag проектора, владение партиями,
  ребалансы, ошибки по RPC.
- SLO: p99 хода < 100 мс, lag проектора < 2 с; alert rules + burn rate.
- Continuous profiling (Pyroscope) для game и Postgres-экспортер.

**Файлы.** `core/shared/pkg/telemetry/`, `core/deploy/observability/`
(collector, prometheus, grafana provisioning, dashboards JSON).

**Готово когда:** в Grafana по одному trace ID виден путь хода через gateway → game → Kafka → проектор.

---

## Фаза 5 — Безопасность уровня production

- mTLS между сервисами (step-ca, короткоживущие сертификаты, автообновление).
- Ротация JWKS по расписанию с двумя активными `kid`, тест на смену ключа под нагрузкой.
- Авторизация как политика: OPA/Rego sidecar или встроенный `policy` с тестами таблиц.
- Аудит-лог безопасности (логин, смена пароля, отзыв сессий) — отдельный топик, append-only.
- `govulncheck`, `npm audit`, Trivy-скан образов, SBOM (syft) и подпись образов (cosign) в CI.

**Готово когда:** сервис без валидного клиентского сертификата не может вызвать другой сервис, и это проверяет e2e.

---

## Фаза 6 — Kubernetes и GitOps

- kind локально; Helm-чарты на сервис; Strimzi (Kafka), CloudNativePG (Postgres),
  Redis Operator.
- HPA по кастомной метрике (активные партии на инстанс), PodDisruptionBudget,
  topology spread; StatefulSet не нужен — владение партиями через consumer group.
- Argo CD синхронизирует `deploy/k8s/` из git; preview-окружение на PR.
- Chaos Mesh вместо Toxiproxy в k8s.

**Готово когда:** `make kind-up` поднимает весь стек, Argo CD зелёный, chaos-сценарии фазы 3 проходят в кластере.

---

## Фаза 7 — Функции, которые показывают архитектуру

- Наблюдатели партии: подписка на поток `game.moves` без влияния на игроков.
- Replay и time-travel любой партии из журнала (слайдер по ходам).
- Турниры: отдельный сервис, сага «регистрация → пары → партии → итоги» с компенсациями.
- Анализ партий: воркер со Stockfish потребляет `game.finished`, пишет оценки ходов.
- SwiftUI-клиент (этап 6 roadmap) поверх тех же контрактов.

---

## Сквозное: качество и контракты

- `buf breaking` против `main` в CI для proto и схем событий.
- AsyncAPI-спека для WS и Kafka, генерируется из proto.
- Property-based тесты движков (`pgregory.net/rapid`): для случайных партий
  `ValidMoves` ⇔ `ApplyMove` без ошибки, терминальность корректна.
- Fuzz-тесты парсеров WS-команд и нотаций (`go test -fuzz`).
- Ночной k6-прогон нагрузки в CI с публикацией результата.

## Порядок и почему

1 → 4 → 3 → 2 → 5 → 6 → 7. Фаза 1 — главное архитектурное решение и самая сложная;
observability (4) нужна сразу после неё, чтобы ребалансы и lag было видно, а не угадывать.
Масштабирование (3) опирается на метрики. Outbox/CDC (2) переиспользует Kafka-кластер
из фазы 1. k8s (6) — после того, как compose-стек стал многоинстансным.

## Критерий готовности всего плана

Chaos-сценарий «убить владельца партии, брокер и инстанс gateway под нагрузкой 500 партий»
проходит без потерянных ходов, а весь путь хода виден одним трейсом в Grafana.
