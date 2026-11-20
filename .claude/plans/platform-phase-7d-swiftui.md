# Фаза 7d — нативный SwiftUI-клиент (iOS + macOS)

## Контекст
Контракты `axon.auth.v1` и `axon.game.v1` после 7a стабильны: вход, лобби, партии трёх игр,
наблюдение и реплей уже работают в вебе. Этап 6 roadmap обещает нативный клиент на тех же
контрактах — это проверка, что API действительно клиент-независимый: Connect отдаёт JSON/proto
любому клиенту, realtime-протокол `axon.game.v1` описан proto-сообщениями, а gateway не держит
веб-специфичных допущений (Origin, cookie).

Результат: приложение `Axon` в `ui/app` (один multiplatform target, iOS 26 / macOS 26) входит
и регистрируется, показывает лобби, создаёт и находит партии, играет крестики-нолики, шахматы и
кворидор через сокет, смотрит чужие партии и листает реплей; XCUITest проходит партию против
Docker-стенда, второй игрок — из теста через HTTP Connect API и WebSocket на `URLSession`.
Звонки и чат в клиент не входят.

## Решение
- **Проект.** XcodeGen `ui/app/project.yml` → `ui/app/Axon.xcodeproj` (git-ignored,
  `make app-project`). Зависимости SPM с точными версиями: `connect-swift` 1.2.3,
  `swift-protobuf` 1.41.0 — те же версии, что у удалённых плагинов buf. Targets: `Axon`
  (application, `supportedDestinations: [iOS, macOS]`), `AxonTests` (unit, Swift Testing),
  `AxonUITests` (XCUITest, iOS). Схемы `Axon` (build + unit) и `AxonUITests`.
  ATS: только `NSAllowsLocalNetworking` (http на localhost/LAN-стенд), macOS — App Sandbox с
  `network.client`.
- **Генерация.** В корневой `buf.gen.yaml` добавлены `buf.build/apple/swift:v1.41.0` и
  `buf.build/connectrpc/swift:v1.2.3` в `ui/app/Gen` (git-ignored) с `Visibility=Internal`,
  `FileNaming=PathToUnderscores` (два `internal.proto` иначе дают одинаковые имена файлов в
  одном модуле), только async-методы. В target входят лишь `axon_auth_v1_auth.*` и
  `axon_game_v1_game.*`; `make proto` перегенерирует всё вместе с Go/TS.
- **`Core/Networking`** — независимо тестируемые типы:
  - `GatewaySettings` — base URL в `UserDefaults` (это не секрет; launch-аргумент
    `-gatewayBaseURL` перекрывает его для XCUITest), по умолчанию `http://localhost:18080`,
    `ws(s)://…/ws/game` выводится из него.
  - `AuthTokenStore` — протокол; `KeychainTokenStore` (generic password, account = base URL:
    токены одного стенда не уходят на другой) и `InMemoryTokenStore` для тестов и превью.
    `UserDefaults` для токенов не используется.
  - `TokenVault` (actor) — текущая пара токенов и single-flight `RefreshToken`: параллельные
    401 дают один refresh; неудачный refresh очищает хранилище и переводит сессию в «вышел».
  - `ConnectClient` — `ProtocolClient` (Connect, JSON) с `BearerInterceptor`, который берёт
    access token из `TokenVault`; каждый вызов при `unauthenticated` делает refresh и повторяет
    запрос один раз. Реализует узкие протоколы `AuthAPI` и `GameAPI`, на которые опираются view
    models (fake в тестах).
  - `WebSocketClient` — `URLSessionWebSocketTask` за протоколом `WebSocketTransport`,
    субпротоколы `axon.game.v1` + `axon.bearer.<token>`, переподключение по `BackoffPolicy`
    (1 с · 2ⁿ, потолок 15 с, джиттер, 15 попыток; код 1008 и 401 на рукопожатии → refresh и
    одна попытка без задержки). `GameChannel` поверх него: `GameSocketCodec` (swift-protobuf
    JSON `SessionCommand`/`SessionEvent`, неизвестные поля игнорируются), подписка
    `subscribe`/`subscribe_match` повторяется после каждого переподключения.
- **Features** — `@Observable @MainActor` view models, во `View` только рендеринг и вызовы:
  - `Auth`: вход/регистрация (`AuthViewModel`), `SessionModel` — восстановление через `GetMe`,
    выход через `Logout` с refresh token в теле (нативный клиент без `Origin` не получает cookie).
  - `Lobby`: каталог игр, «Ваши партии» (`ListSessions`), «Идут сейчас» (`ListLiveSessions` без
    своих), приватная партия (`CreateSession` с `request_id`), вход по id (`GetSession`, при
    `NotFound` — `JoinSession`), быстрый матч (`FindMatch` + `subscribe_match` до `MATCHED`,
    `CancelMatch`). Имена соперников — `GetUsersPublicProfiles`.
  - `Games`: `MatchViewModel` — состояние только из `Session`: снапшоты по `revision`, ход
    уходит с `ply`/`revision`, `pending` до следующей ревизии, часы из `SessionClock` с
    отсчётом от момента получения, сдача (`LeaveSession`), ничья (`DrawCommand`) только в
    шахматах — как в вебе. Зритель (не участник при двух местах) видит партию без управления.
    Доски — `TicTacToe`, `Chess`, `Quoridor`, у каждой свой view model; подсвечиваются только
    ходы из `legal_moves` сервера (для крестиков — пустые клетки в свой ход), FEN разбирается
    лишь для расстановки фигур. Кворидор: режим «пешка/стена», стена выбирается касанием паза
    (`WallPlacementGesture` — чистая геометрия касание → якорь стены) и подтверждается.
  - `Replay`: `GetSessionHistory` → кадры, слайдер и кнопки «в начало/назад/вперёд/в конец»,
    кто ходил, итог партии.
  - `Settings`: base URL с проверкой схемы и хоста, аккаунт, выход.
- **Платформы.** `AdaptiveNavigation` — единственная развилка: `NavigationSplitView` (sidebar +
  `NavigationStack` в detail) на macOS и `NavigationStack` на iOS. `#if os(macOS)` больше
  нигде, кроме размера окна в `AxonApp`.
- **Gateway и Origin.** Нативный клиент не шлёт `Origin`: `cors.Middleware` пропускает запросы
  без него (403 только для чужого Origin), cookie-интерсептор для них не трогает refresh token,
  `coder/websocket` принимает рукопожатие без `Origin`. Код gateway не меняется — проверяется
  curl-запросом без `Origin` и самим XCUITest.
- **Стенд для XCUITest.** `make test-e2e-app`: проект `axon-app-e2e`, базовый compose +
  `core/deploy/docker-compose.app-e2e.yml` (healthcheck-и через `/service -healthcheck`,
  щедрые лимиты gateway), `BUILD_TARGET=runtime`, порты семейства x6
  (gateway `18680`, …), `up --wait gateway game`, `xcodebuild test -scheme AxonUITests` на
  iPhone 18 Pro с `TEST_RUNNER_AXON_E2E_BASE_URL`, затем `down -v`.
- **CI.** Job `app` на `macos-latest`: последняя стабильная Xcode, `buf generate`, XcodeGen,
  swiftlint, `xcodebuild build` для iOS Simulator и `xcodebuild test` на macOS.

## Файлы
- `buf.gen.yaml`, `.gitignore`, `Makefile` (`app-project`, `app-build`, `app-test`, `app-lint`,
  `test-e2e-app`), `.github/workflows/ci.yml`, `core/shared/proto/README.md`, `README.md`.
- `core/deploy/docker-compose.app-e2e.yml`.
- `ui/app/project.yml`, `ui/app/.swiftlint.yml`, `ui/app/App/Info.plist`.
- `ui/app/App/` — `AxonApp`, `AppModel` (composition root), `RootView`, `Router`.
- `ui/app/Core/Networking/` — `GatewaySettings`, `AuthTokenStore`, `TokenVault`,
  `ConnectClient`, `BearerInterceptor`, `APIError`, `WebSocketClient`, `BackoffPolicy`,
  `GameSocketCodec`, `GameChannel`.
- `ui/app/Core/Models/` — typealias-ы на сгенерированные типы и `GameKind`.
- `ui/app/Features/{Auth,Lobby,Games,Settings}/…`, `ui/app/Features/Games/{TicTacToe,Chess,
  Quoridor,Replay}/…`, `ui/app/Shared/UI/` (`AdaptiveNavigation`, общие элементы).
- `ui/app/Tests/AxonTests/…`, `ui/app/UITests/AxonUITests/…`.

## Как проверяется
- `make proto && make app-project` — генерация Swift и проекта без ошибок.
- `xcodebuild -project ui/app/Axon.xcodeproj -scheme Axon -destination 'generic/platform=iOS Simulator' build`
  и то же для `platform=macOS` — 0 ошибок, 0 предупреждений в коде приложения.
- `xcodebuild … -scheme Axon -destination 'platform=macOS' test` — unit-тесты view models на
  fake-сети, `KeychainTokenStore`/`InMemoryTokenStore`, `GameSocketCodec`, `BackoffPolicy`,
  переподключение `WebSocketClient`, refresh в `ConnectClient`.
- `swiftlint lint --strict` в `ui/app`.
- `make test-e2e-app` — XCUITest: регистрация, выход и вход, приватная партия в крестики,
  второй игрок из теста (Connect JSON + WebSocket на `URLSession`) присоединяется и отвечает,
  ходы соперника приходят вживую, «You won», реплей с позицией на нужном ходу; стенд снесён.
- Скриншоты симулятора (`xcrun simctl io booted screenshot`) лобби, партии и реплея, плюс окно
  macOS — без явных багов вёрстки.

## Критерий готовности
`make test-e2e-app` зелёный: приложение на симуляторе iPhone 18 Pro играет партию против
Docker-стенда, видит ходы второго клиента и открывает её реплей.
