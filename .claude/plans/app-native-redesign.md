# Приложение: нативный редизайн (Liquid Glass) и вход через GitHub

## Контекст
- Владелец посмотрел приложение на iPhone (2026-11-21): стандартные системные формы ему не нравятся, входа через
  провайдеры нет, проверки в симуляторе перед телефоном не было.
- Решение владельца 2026-11-21: переделать всё приложение (вход, лобби, партии, реплей, настройки)
  в нативном стиле iOS 26 (Liquid Glass) с брендом Axon и добавить вход через GitHub. Проверка — сначала
  в симуляторе со скриншотами, телефон потом.

Результат: в симуляторе «Continue with GitHub» приводит в приложение, все экраны в новом стиле, тесты зелёные.

## Ветка
`feat/app-native-redesign` от `feat/auth-provider-linking` (#53): в `auth.proto` номера полей идут после `link = 3`.
При /ship auth и web-мост можно отделить в `feat/auth-app-oauth`.

## Вход из приложения (RFC 8252)
- Proto `auth.proto` (additive): `StartOAuthRequest.optional string app_challenge = 4`,
  `CompleteOAuthRequest.optional string app_verifier = 5`.
- Auth:
  - `oauth.Registry` держит второй экземпляр каждого провайдера, кроме Apple, с redirect
    `<OAUTH_REDIRECT_BASE_URL>/<provider>/app`. Это подпуть уже зарегистрированного callback, настройки
    GitHub-приложения не меняются.
  - `oauth.State.AppChallenge`. `StartOAuth` с `app_challenge` берёт app-провайдера и запоминает challenge.
  - `CompleteOAuth` для такого состояния требует `app_verifier`, у которого S256 совпадает с challenge
    (сравнение за постоянное время), иначе `ErrOauthStateInvalid`. Код меняется через app-провайдера.
  - Перехват `axon://` чужим приложением не даёт войти: у него нет verifier. Привязка из приложения не делается.
- Web: `ui/web/src/app/auth/callback/[provider]/app/route.ts` — GET отвечает 302 на
  `axon://oauth/<provider>?code=…&state=…` (или `error=…`), только для известных провайдеров.
- Приложение: `WebAuthenticationSession` (SwiftUI, AuthenticationServices) с callback `.customScheme("axon")`,
  verifier/challenge через CryptoKit, затем `CompleteOAuth` → токены в Keychain как при входе паролем.

## Дизайн (Liquid Glass, бренд Axon)
- Акцент — фиолетовый Axon (`BoardPalette.violet`), светлая и тёмная темы.
- Корень после входа — `TabView` со стилем `.sidebarAdaptable`: Play / Games / Live / Profile, в каждой вкладке
  свой `NavigationStack`, партия и реплей открываются внутри вкладки. Стеклянный таб-бар даёт система.
- Вход: фирменный фон (`MeshGradient`), знак и название Axon, стеклянная карточка: «Continue with GitHub»,
  переключатель Sign in / Register, поля, кнопка `.glassProminent`.
- Play: плитки игр (символ, название, Quick match — `.glassProminent`, New game — `.glass`), стеклянный баннер
  поиска соперника с отменой, Join by ID.
- Games: ваши текущие и завершённые партии. Live: идущие партии для просмотра. Profile: аккаунт, адрес gateway,
  выход.
- Партия: карточки игроков с часами-«таблетками», доска по центру, действия (ничья, отмена хода, сдаться, реплей,
  поделиться ID) в стеклянной панели снизу; ходы — горизонтальная лента под доской на iPhone и список в боковой
  панели на широком экране. Маркеры игроков крестиков-ноликов — цвета меток доски.
- Реплей: стеклянные кнопки перемотки.
- Не меняются: view models, сеть, доски (кроме цветов рамки), `AccessibilityID`, на которые опираются XCUITest.

## Проверка
- `go test ./...` и `golangci-lint run ./...` в `core/services/auth` и `core/services/gateway`.
- `ui/web`: `npm run typecheck && npm run lint && npm test`.
- `xcodebuild test -scheme Axon` на iPhone 18 Pro (simulator); XCUITest `TicTacToeFlowTests` против стенда.
- Стенд: пересобрать auth, gateway, web; симулятор iPhone 18 Pro: скриншоты всех экранов в светлой и тёмной теме,
  вход через GitHub до лобби (владелец сам входит в GitHub в окне входа).

## Расхождения с замыслом
- `AdaptiveNavigation` и `LobbyView` удалены: навигацию даёт `TabView` со стилем `.sidebarAdaptable`, лобби разделено
  на Play и списки Games/Live.
- Gateway не пересобирался: приложение ходит Connect ProtoCodec, новые поля proto проходят через него как неизвестные.
- Живой вход через GitHub в симуляторе отложен владельцем; поток покрыт unit-тестами Swift, Go service-тестами и e2e
  auth (`TestTheAppSignsInWithItsVerifier`, `TestAnInterceptedAppCallbackCannotSignIn`).

## Критерий готовности
В симуляторе вход через GitHub ведёт в новое приложение, все экраны в стиле Liquid Glass, Go/web/Swift тесты зелёные.
