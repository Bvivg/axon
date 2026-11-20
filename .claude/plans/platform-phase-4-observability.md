# Фаза 4 — observability полного цикла

## Контекст
После фаз 1/1b game — stateful-сервис с журналом в Kafka, владением партициями и проектором.
Ребалансы, lease, отставание проектора и задержку хода сейчас видно только по логам и разовым
замерам нагрузчика. Нужна сквозная картина: один trace от клика в браузере до записи проектора,
метрики домена с exemplars, логи с trace_id, SLO с burn-rate алертами и профили CPU.

Результат: overlay `docker-compose.observability.yml` поднимает OTel Collector, Tempo, Prometheus,
Loki, Alloy, Pyroscope, postgres-exporter и Grafana с готовыми дашбордами и алертами; по trace ID
хода в Grafana виден путь браузер → gateway → game → Kafka → проектор.

## Решение
- **`shared/pkg/telemetry`**: OTLP gRPC exporter, `ParentBased(TraceIDRatio)` (доля из
  `OTEL_TRACES_SAMPLER_ARG`), W3C tracecontext + baggage, `otelconnect` для всех Connect-серверов и
  клиентов, `otelhttp`-транспорт для пересылки между инстансами game, pprof по `PPROF_ENABLED`.
  Без `OTEL_EXPORTER_OTLP_ENDPOINT` всё no-op — локальный запуск и тесты не меняются.
- **WebSocket**: команда `SessionCommand` получила `optional string traceparent = 15`.
  Web кладёт новый sampled traceparent в каждую команду; gateway (`internal/wstrace`) открывает
  спан `gateway.ws <команда>` и подменяет traceparent в кадре на свой; game открывает
  `game.command <команда>` дочерним к нему.
- **Kafka**: `journal.append` (producer) пишет traceparent в заголовки записи; проектор после
  коммита пакета создаёт `projector.apply` дочерним к каждой sampled записи (со временем начала
  пакета). Общий `shared/pkg/kafka` (chat/auth) так же пишет и читает заголовки (`publish`/`process`).
- **Логи**: `logger` добавляет `trace_id`/`span_id` из контекста; Alloy читает логи контейнеров
  проекта `axon` и отправляет в Loki, `service` и `level` — метки.
- **Метрики**: RPC-гистограммы получают exemplars с `trace_id`, `/metrics` отдаёт OpenMetrics.
  Домен game (`internal/metrics`): команды и их задержка (с exemplars), ходы по играм, задержка
  записи в журнал, владение партициями, claim/revoke по причинам, продления lease, активные
  партии по играм (метрика для будущего HPA), проектор: записи, размер пакета, длительность,
  отставание в записях и секундах.
- **SLO**: 99% команд быстрее 100 мс — multi-window multi-burn-rate алерты (14,4× за 1 ч и 5 мин,
  6× за 6 ч и 30 мин); отставание проектора > 2 с в течение минуты; доля ошибок RPC > 5%.
- **Сеть `obs`**: новая зона ответственности (сбор телеметрии). Сервисы подключаются к ней только
  в overlay; Prometheus/Alloy/Collector не видят `data`, кроме postgres-exporter.

## Файлы
- `core/shared/pkg/telemetry/` — новый; `core/shared/pkg/logger/logger.go`,
  `core/shared/pkg/middleware/metrics.go`, `core/shared/pkg/kafka/{producer,consumer,message}.go`.
- `core/shared/proto/axon/game/v1/game.proto` — `traceparent` в `SessionCommand`.
- `core/services/game/internal/metrics/` — новый; `owner`, `journal/kafka.go`, `cluster`,
  `server/{socket,handler}.go`, `cmd/game/main.go`.
- `core/services/gateway/internal/{wsproxy,wstrace}/`, `cmd/gateway/main.go`; `auth`, `chat` main.
- `ui/web/src/lib/ws/{traceparent.ts,use-game-socket.ts}`.
- `core/deploy/docker-compose.observability.yml`, `core/deploy/observability/**`.

## Как проверяется
- `go test -race ./...` во всех модулях, `make lint`, vitest/tsc/eslint web.
- Юнит: `wstrace` (спан gateway — потомок клиентского, кадр несёт спан gateway),
  `telemetry` (round trip traceparent), `logger` (trace_id в строке лога).
- `docker compose ... -f docker-compose.observability.yml config -q`; promtool для правил.
- Стенд с overlay: ход в браузере → trace ID из кадра → в Tempo трейс со спанами
  `gateway.ws`, `game.command`, `journal.append`, `projector.apply`; в Grafana дашборды заполнены,
  exemplar ведёт в трейс, лог с тем же trace_id находится в Loki, профиль game есть в Pyroscope.

## Критерий готовности
В Grafana по одному trace ID виден путь хода gateway → game → Kafka → проектор.
