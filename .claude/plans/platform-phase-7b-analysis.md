# Фаза 7b — анализ шахматных партий Stockfish

## Контекст
После 7a у любой завершённой партии есть реплей по ходам, но игрок не видит, где он ошибся.
Анализ движком — естественный асинхронный потребитель `game.finished.v1`: тяжёлая работа вне
игрового сервиса, своя схема, идемпотентность по событию, повторная доставка без дублей. Это
показывает ту же цепочку outbox → CDC → Kafka, что и rating, но с CPU-тяжёлым воркером и пулом
внешних процессов.

Результат: после мата сервис `analysis` за секунды пишет оценку каждой позиции и класс каждого
хода; в реплее шахмат видна шкала оценки, график по ходам и пометки `?!`/`?`/`??` в списке ходов.

## Решение
- **Ходы едут в событии.** `axon.events.v1.GameFinished` получает `repeated string moves = 9`
  (additive). Game заполняет его в `projection/finished.go` через `chess.Moves(state)` — UCI-ходы
  из состояния партии; остальные игры оставляют поле пустым. Анализу не нужен синхронный вызов
  `GetSessionHistory` и чтение чужой схемы (отклонено: лишняя связность и зависимость от
  доступности game в момент обработки).
- **Сервис `core/services/analysis`** — отдельный модуль в `go.work`, схема и роль
  `analysis`/`analysis_service`. Пакеты:
  - `internal/uci` — процесс Stockfish по UCI: `uci`/`isready`, `setoption Threads 1`,
    `Hash 16`, `ucinewgame`, `position startpos moves …`, `go depth N`, разбор `info … score
    cp|mate` и `bestmove`; таймаут на позицию — `stop`, затем убийство процесса. `Pool` из
    `ANALYSIS_ENGINES` процессов (по умолчанию 2) ограничивает параллелизм; упавший или
    зависший процесс заменяется новым (`analysis_engine_restarts_total`).
  - `internal/review` — чистая логика: перевод оценки в шкалу белых (мат → ±10000), потеря
    сантипешек с точки зрения походившего, классы: `best` — ход совпал с лучшим ходом движка,
    иначе `blunder` ≥ 300, `mistake` ≥ 100, `inaccuracy` ≥ 50, `good` < 50.
  - `internal/analyser` — позиции партии оцениваются параллельно на движках пула
    (`ANALYSIS_DEPTH`, по умолчанию 12; `ANALYSIS_POSITION_TIMEOUT`, по умолчанию 10 с), одна
    повторная попытка после перезапуска движка.
  - `internal/store` — `reports` (pending/ready/failed, глубина, движок, время начала/конца,
    длительность, ошибка), `positions` (ply, оценка cp/мат в шкале белых, лучший ход, сыгранный
    ход, потеря, класс), `processed_events`.
  - `internal/consumer` — группа `analysis` на `game.finished.v1`: не-шахматы и уже обработанные
    события пропускаются; сначала пишется `pending`-отчёт, затем анализ, затем одной транзакцией
    `processed_events` + позиции + `ready` (или `failed` с ошибкой). Вставка в
    `processed_events` с `ON CONFLICT DO NOTHING` — гонка двух доставок не даёт дублей. Отмена
    контекста при остановке не превращается в `failed`: запись не коммитится и придёт снова.
    Шахматное событие без ходов (до 7b) — `failed`.
  - `internal/server` — `axon.analysis.v1.AnalysisService/GetAnalysis(session_id)`: статус и
    позиции; JWT проверяется локально через JWKS; партии публичны для просмотра, поэтому
    читать может любой вошедший. Нет отчёта — `NotFound`.
- **Метрики**: `analysis_jobs_total{outcome}` (ready/failed/duplicate/skipped/invalid),
  `analysis_duration_seconds`, `analysis_engine_restarts_total`, плюс общие RPC-метрики.
  Дашборда у rating нет, поэтому панель Grafana не добавляется; Prometheus получает job.
- **Образ**: в `Dockerfile.service` flavor `runtime-analysis` — `alpine` + `apk add stockfish`,
  непривилегированный пользователь 65532; путь к бинарю — `STOCKFISH_PATH`.
- **Gateway**: прокси `AnalysisService`, запись в `policy` (стандартный tier, только вошедшие),
  `GetAnalysis` в списке повторяемых процедур, `ANALYSIS_SERVICE_URL`.
- **Web**: в реплее шахмат `useAnalysis` (TanStack Query) спрашивает `GetAnalysis` у
  завершённой партии и опрашивает каждые 3 с, пока отчёт `pending` или ещё не создан; шкала
  оценки рядом с доской для текущего кадра, SVG-график оценки по ply с маркером текущего хода,
  пометки `?!`/`?`/`??` в списке ходов, состояние «Analysis pending». Реплей крестиков-ноликов и
  кворидора не меняется.

## Файлы
- `core/shared/proto/axon/events/v1/events.proto`, `core/shared/proto/axon/analysis/v1/analysis.proto`.
- `core/services/game/internal/games/chess/chess.go` (`Moves`), `core/services/game/internal/projection/finished.go`.
- `core/services/analysis/**` — новый модуль: `cmd/analysis`, `internal/{uci,review,analyser,store,consumer,server}`,
  `migrations/000001_analysis.*.sql`, `integration/`, `e2e/`.
- `core/services/gateway/{cmd/gateway/main.go,internal/proxy/analysis.go,internal/policy/*,internal/config/config.go}`.
- `core/deploy/Dockerfile.service`, `docker-compose.yml` (`analysis`, `analysis-migrate`, пароль роли),
  `docker-compose.{ha,mtls,mtls-e2e,observability,e2e,backup,web-e2e}.yml`, новый
  `docker-compose.analysis-e2e.yml`, `postgres/init/01-schemas.sh`, `.env.example`,
  `observability/prometheus/prometheus.yml`, `cdc/setup.sh` не меняется (у analysis нет outbox).
- Пароль роли во всех integration-тестах, которые запускают `01-schemas.sh`
  (`core/services/{auth,chat,game,rating}/integration`, где есть).
- `Makefile`, `core/deploy/docker-compose.tools.yml`, `.github/workflows/ci.yml`, `scripts/start-dev.mjs`.
- `ui/web/src/lib/connect/clients.ts`, `ui/web/src/lib/query/analysis.ts`,
  `ui/web/src/components/games/chess/{analysis,moves}.tsx`, `ui/web/src/components/games/replay.tsx`,
  `ui/web/e2e/analysis.spec.ts`.

## Как проверяется
- `make lint` — 0 замечаний; `go test -race` в `shared`, `game`, `gateway`, `analysis`.
- Unit: разбор UCI на записанном выводе Stockfish, классификация и потеря (table-driven),
  консьюмер с фейковым хранилищем и анализатором, `chess.Moves`, событие `GameFinished` с ходами,
  политика gateway покрывает новый RPC.
- Integration: `go test -tags integration ./services/analysis/integration/...` (testcontainers:
  Postgres с `01-schemas.sh` и миграциями, Kafka) — pending → ready, повторная доставка того же
  события в Kafka не добавляет строк; движок — тот же тег в контейнере с `apk add stockfish`
  (детский мат, перезапуск убитого процесса).
- E2E: `make test-e2e-analysis` — проект `axon-analysis-e2e` на базовом стеке + CDC: детский мат
  `e2e4 e7e5 f1c4 b8c6 d1h5 g8f6 h5f7` через сокет gateway, `GetAnalysis` через gateway до
  `ready`; ply 6 (`g8f6`) — `blunder`, последняя позиция — мат, позиций = ply + 1; затем `down -v`.
- Web: `tsc`, `eslint`; Playwright `e2e/analysis.spec.ts` на `docker-compose.web-e2e.yml`
  (проект `axon-web-e2e-p7b`): шкала оценки и пометка `??` у `Nf6`; затем `down -v`.

## Критерий готовности
Детский мат, сыгранный через gateway, через несколько секунд показывает в реплее зевок `Nf6??`
и мат на последнем ходу, а повторная доставка события не меняет число строк анализа.
