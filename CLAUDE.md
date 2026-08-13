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
- **Сервисы**: `gateway`, `auth`, `chat`, `game`, `calling`
- **БД**: Postgres — единая база, schema-level изоляция между сервисами
- **Кэш**: Redis — только кэширование/presence, не источник истины
- **Event bus**: Kafka — межсервисные события (`game.started`, `chat.message`, ...)
- **Протоколы**:
  - gRPC — между внутренними сервисами
  - Connect (buf.build) — на gateway, один handler отдаёт gRPC + gRPC-Web + JSON
  - `coder/websocket` — realtime транспорт (ходы в играх, сообщения чата)
- **Calling**: LiveKit (SFU) — медиапоток идёт напрямую клиент ↔ LiveKit,
  минуя gateway; `calling` сервис — тонкий оркестратор (токены, комнаты, вебхуки)
- **Auth**: собственный password + refresh flow, плюс OAuth2 (Google, GitHub, Apple),
  RS256/JWKS JWT с локальной верификацией токена (без похода в auth на каждый запрос)

### UI (`ui/`)
- **`app/`** — SwiftUI, единый multiplatform target (iOS + macOS, различия через `#if os(macOS)`)
  - Connect-Swift для RPC, WebSocket для realtime, LiveKit client SDK для звонков
- **`web/`** — Next.js
  - `@connectrpc/connect-web` для RPC, `react-use-websocket` для realtime
  - shadcn + @base-ui + Tailwind — UI
  - TanStack (query/table/virtualizer) — данные
  - axios — fallback HTTP

### Docs (`docs/`)
- Архитектурные диаграммы, API-контракты
- `schema.jpg` — диаграмма архитектуры (сгенерирована Gemini), лежит рядом с этим файлом

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

Quoridor — самая нетривиальная игра в наборе: доска 9x9, стены между клетками
моделируются как граф с "перерезанными" рёбрами, каждое размещение стены
требует BFS-проверки, что у обоих игроков остаётся путь до финиша.

## Структура папок

```
axon/
├── core/
│   ├── go.work
│   ├── services/
│   │   ├── gateway/
│   │   │   ├── go.mod
│   │   │   ├── cmd/main.go
│   │   │   └── internal/
│   │   │       ├── connect/        # Connect handlers (gRPC + gRPC-Web + JSON)
│   │   │       └── router/
│   │   ├── auth/
│   │   │   ├── go.mod
│   │   │   └── internal/
│   │   │       ├── jwt/            # RS256/JWKS
│   │   │       ├── oauth/          # Google/GitHub/Apple
│   │   │       └── repository/
│   │   ├── chat/
│   │   │   ├── go.mod
│   │   │   └── internal/
│   │   │       ├── ws/
│   │   │       └── pubsub/         # Redis fan-out
│   │   ├── game/
│   │   │   ├── go.mod
│   │   │   ├── cmd/main.go
│   │   │   └── internal/
│   │   │       ├── server/         # grpc.go, ws.go
│   │   │       ├── session/        # session.go, repository.go, manager.go
│   │   │       ├── engine/         # engine.go (Game interface), registry.go
│   │   │       └── games/
│   │   │           ├── chess/
│   │   │           ├── durak/
│   │   │           ├── checkers/
│   │   │           ├── tictactoe/
│   │   │           └── quoridor/
│   │   │               ├── board.go
│   │   │               ├── move.go
│   │   │               ├── pathfinding.go   # BFS проверка пути
│   │   │               ├── rules.go
│   │   │               └── engine.go
│   │   └── calling/
│   │       ├── go.mod
│   │       └── internal/
│   │           └── sfu/            # LiveKit integration
│   ├── shared/
│   │   ├── proto/                  # единый источник правды для всех клиентов
│   │   ├── pkg/
│   │   │   ├── kafka/
│   │   │   ├── postgres/
│   │   │   ├── redis/
│   │   │   ├── logger/
│   │   │   ├── middleware/
│   │   │   └── ws/
│   │   └── config/
│   └── deploy/
│       ├── docker-compose.yml
│       └── k8s/
├── ui/
│   ├── app/
│   │   ├── App/
│   │   │   └── AxonApp.swift
│   │   ├── Core/
│   │   │   ├── Networking/         # ConnectClient, WebSocketClient, AuthTokenStore
│   │   │   └── Models/
│   │   ├── Features/
│   │   │   ├── Auth/
│   │   │   ├── Lobby/
│   │   │   ├── Games/
│   │   │   │   ├── Chess/
│   │   │   │   ├── Durak/
│   │   │   │   └── Quoridor/
│   │   │   │       ├── QuoridorBoardView.swift
│   │   │   │       ├── QuoridorViewModel.swift
│   │   │   │       └── WallPlacementGesture.swift
│   │   │   └── Calling/
│   │   └── Shared/UI/
│   └── web/
│       ├── app/
│       │   ├── (auth)/
│       │   ├── (lobby)/
│       │   └── games/quoridor/[sessionId]/page.tsx
│       ├── components/
│       │   ├── ui/                 # shadcn + @base-ui
│       │   └── games/quoridor/
│       │       ├── Board.tsx
│       │       ├── Wall.tsx
│       │       └── useQuoridorSocket.ts
│       ├── lib/
│       │   ├── connect/            # @connectrpc/connect-web клиент
│       │   ├── ws/
│       │   └── query/              # TanStack query hooks
│       └── shared-types/
└── docs/
    ├── architecture.md
    ├── schema.jpg               # диаграмма архитектуры (Gemini)
    └── api/
```

## Правила разработки

**Важно: ничего не выполняется локально напрямую — только через Docker (`docker-compose`). Детали в `rules/infra.md`.**

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

## Открытые вопросы / TODO
- Реализация `pathfinding.go` (BFS-валидация стен Quoridor)
- Proto-контракты для game service под текущую структуру
- Деплой: docker-compose локально, k8s — по возможности
