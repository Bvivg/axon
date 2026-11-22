# Axon

Портфолио-проект: модульный микросервисный монорепо на Go с играми
(Durak, Chess, Checkers, Tic-Tac-Toe, Quoridor) в качестве основной
функциональности, плюс realtime-чат и видеозвонки.

## Идея

Единая платформа с несколькими клиентами (desktop/mobile на SwiftUI,
web на Next.js), общающимися через единый API Gateway с backend-сервисами
на Go. Игровой движок построен вокруг общего интерфейса `Game`, так что
добавление новой игры (например, Quoridor) не требует переделки
инфраструктуры — только реализации домена самой игры.

## Стек

### Backend (`core/`)
- **Язык**: Go, `go.work` — мультимодульный монорепо
- **Сервисы**: `gateway`, `auth`, `chat` (с модулем Calls), `game`, `rating`, `tournament`, `analysis`
- **БД**: Postgres — единая база, schema-level изоляция между сервисами
- **Кэш**: Redis — только кэширование/presence, не источник истины; у gateway отдельный `edge-redis` под rate limit
- **Event bus**: Kafka — межсервисные события (`game.finished.v1`, `chat.message.v1`, `rating.changed.v1`, ...) через outbox + Debezium; живые партии — журнал `game.session-events.v1`
- **Протоколы**:
  - gRPC — между внутренними сервисами
  - Connect (buf.build) — на gateway, один handler отдаёт gRPC + gRPC-Web + JSON
  - `coder/websocket` — realtime транспорт (ходы в играх, сообщения чата)
- **Calling**: модуль Calls внутри Chat (ADR-1, решение владельца 2026-11-03), схема `chat`.
  1:1 — native WebRTC + coturn; группы — self-hosted LiveKit только в отдельно разрешаемом Рубеже 2.
  Отдельный `calling` сервис из прежнего roadmap/дерева не создаётся. Граница будущего выделения —
  `axon.calls.v1` и интерфейсы Store/Signaling/MediaProvider; подробности в `.claude/plans/calls-hybrid.md`.
- **Auth**: собственный password + refresh flow, плюс OAuth2 (Google, GitHub, Apple),
  RS256/JWKS JWT с локальной верификацией токена (без похода в auth на каждый запрос)

### UI (`ui/`)
- **`app/`** — SwiftUI, единый multiplatform target (iOS + macOS, различия через `#if os(macOS)`)
  - Connect-Swift для RPC, WebSocket для realtime; сейчас только игры (без чата, звонков и Durak)
- **`web/`** — Next.js
  - `@connectrpc/connect-web` для RPC, `react-use-websocket` для realtime
  - shadcn + @base-ui + Tailwind — UI
  - TanStack (query/table/virtualizer) — данные
  - axios — fallback HTTP
- **`admin/`** — Next.js-админка, отдельное приложение (`basePath: /admin`), web проксирует `/admin` на неё
  - тот же стек, что у web, плюс TanStack Form + protovalidate: CRUD с update_mask, etag и аудитом

### Docs (`docs/`)
- `architecture.md` — архитектура и диаграммы, `adr/` — архитектурные решения, `benchmarks.md` — замеры

## Игровой движок (`core/services/game/internal/engine`)

Общий интерфейс, который реализует каждая игра:

```go
type Game interface {
    Init(players []PlayerID) State
    ApplyMove(state State, move Move) (State, error)
    ValidMoves(state State, player PlayerID) []Move
    IsTerminal(state State) (bool, *PlayerID)
}
```

Игры со скрытой информацией (Durak) дополнительно реализуют опциональные интерфейсы:
`Seeded` (`InitSeeded(players, seed)` — колода из seed, который сессия берёт из `crypto/rand`
и записывает в журнал) и `Viewer` (`View(state, viewer)` — проекция состояния на зрителя
на всех границах). Остальные игры их не реализуют, `Game` не меняется.

Quoridor — самая нетривиальная из открытых игр: доска 9x9, стены между клетками
моделируются как граф с "перерезанными" рёбрами, каждое размещение стены
требует BFS-проверки (`pathfinding.go`), что у обоих игроков остаётся путь до финиша.

## Структура папок

```
axon/
├── core/
│   ├── go.work
│   ├── services/
│   │   ├── gateway/        # Connect + WebSocket прокси, JWKS, CORS, rate limit
│   │   ├── auth/           # пароль, refresh, OAuth (Google/GitHub/Apple), RBAC, JWKS
│   │   ├── chat/           # комнаты, сообщения, медиа, поиск, push, модуль Calls
│   │   ├── game/
│   │   │   ├── cmd/        # game, loadgen
│   │   │   └── internal/
│   │   │       ├── engine/         # Game, Seeded, Viewer, registry
│   │   │       ├── games/          # chess, checkers, durak, quoridor, tictactoe
│   │   │       ├── journal/        # Kafka-журнал ходов
│   │   │       ├── owner/          # владельцы партиций, lease, fencing
│   │   │       ├── projection/     # read model в Postgres
│   │   │       ├── session/  server/  matchmaking/  tournament/  rating/
│   │   ├── rating/         # Glicko-2, лидерборд
│   │   ├── tournament/     # сага турниров
│   │   └── analysis/       # Stockfish-разбор шахмат
│   ├── shared/
│   │   ├── proto/          # единый источник правды для всех клиентов
│   │   ├── gen/            # сгенерировано buf, не редактируется
│   │   └── pkg/            # authn, correlation, kafka, logger, middleware, mtls, outbox, postgres, redis, telemetry, ws, ...
│   └── deploy/             # docker-compose*.yml, Dockerfile.*, k8s/ (kind + Argo CD), chaos/, cdc/
├── ui/
│   ├── app/                # SwiftUI: App/, Core/{Models,Networking}, Features/{Auth,Lobby,Game,Games,Replay,Settings}
│   ├── web/                # Next.js: src/app, src/components (games/<game>/, chat/, calls/), src/lib, e2e/
│   └── admin/              # Next.js-админка на /admin: src/app/(console), src/components/admin, src/lib
└── docs/                   # architecture.md, adr/, benchmarks.md, calls.md
```

## Правила разработки

**Полный dev-запуск — `npm run start:dev` из корня: Next.js и все Go-сервисы работают локально на устройстве, инфраструктурные зависимости — в Docker. Перед передачей пользователю на ревью обязательно собрать и проверить результат в Docker Compose; ссылку для ревью давать только на Docker-стенд. Детали в `.claude/rules/infra.md` и `.claude/rules/testing.md`.**

Детальные конвенции по каждой части стека — в `rules/`:
- `rules/go-services.md` — структура пакетов, ошибки, тесты, игровой движок
- `rules/api-contracts.md` — proto/Connect, совместимость, именование
- `rules/swiftui-app.md` — multiplatform-архитектура, view models, хранилище
- `rules/nextjs-web.md` — структура, данные (Connect-Web/TanStack), UI
- `rules/infra.md` — Postgres/Redis/Kafka/LiveKit/Docker
- `rules/testing.md` — unit/integration/e2e, e2e обязателен на каждую крупную фичу
- `rules/git-ci.md` — коммиты, ветки, PR, GitHub Actions pipeline
- `rules/observability.md` — структурированные логи, correlation ID, метрики, трейсинг, health checks
- `rules/security.md` — секреты, gateway как граница доверия, rate limiting, JWT/JWKS
- `rules/comments.md` — в коде не пишем комментарии (и почему)
- `rules/mcp-tools.md` — доступные MCP-серверы и когда их использовать вместо ручных команд

## Открытые вопросы
Известные пробелы ведутся в README (раздел «Known gaps») и в `.claude/plans/roadmap.md`.
