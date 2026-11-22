# Привязка провайдеров входа к аккаунту

## Контекст
- Пользователь с паролем не может войти через GitHub с тем же email: `resolveOauthUser` отказывает, если email
  существующего аккаунта не подтверждён (защита от захвата через чужой незаверенный адрес), а подтвердить email
  или привязать провайдер в продукте нечем. На сайте это выглядит как «вход через GitHub не работает».

Результат: вошедший пользователь привязывает GitHub из профиля; при отказе «аккаунт уже есть» сайт объясняет,
как это сделать.

## Ветка `feat/auth-provider-linking`
Proto (`core/shared/proto/axon/auth/v1/auth.proto`, только additive):
- `StartOAuthRequest.optional bool link = 3` — старт привязки, требует access token.
- `CompleteOAuthResponse.optional bool linked = 4` — привязка без выдачи новых токенов.
- `GetMeResponse.repeated OauthProvider linked_providers = 2`.

Auth (`core/services/auth`):
- `oauth.State.LinkUserID`; `StartOAuth(ctx, provider, returnTo, linkUser *uuid.UUID)`.
- `CompleteOAuth`: состояние привязки принимается только от того же пользователя (access token в заголовке),
  иначе `ErrOauthStateInvalid` — защита от подсовывания чужой привязки. Провайдерская личность, занятая
  другим аккаунтом, — `ErrOauthIdentityClaimed`. Токены не выдаются, `linked = true`.
- `Me` отдаёт список привязанных провайдеров (`store.OauthAccountsByUser`).
- Handler: `StartOAuth`/`CompleteOAuth` аутентифицируют заголовок, когда нужно привязывать.

Web (`ui/web/src`):
- Профиль: блок «Sign-in methods» с GitHub — Connect / Connected.
- Callback-страница: ответ `linked` → назад в профиль без смены сессии.
- Ошибка «account exists» → подсказка «войдите паролем и привяжите провайдер в профиле».

## Отложено
Вход через GitHub в SwiftUI-приложении (RFC 8252: системная сессия, PKCE от клиента, `<base>/<provider>/app` → `axon://`) и новый экран входа приложения — отложены владельцем 2026-11-21. Callback GitHub на стенде остаётся `http://localhost:3003`.

## Проверка
- `go test ./...` в `core/services/auth`, `core/services/gateway`; `npm run typecheck && npm test` в `ui/web`.
- Стенд Docker: на http://localhost:3003 войти паролем → профиль → Connect GitHub → «Connected»; выйти → войти
  через GitHub; Playwright e2e на fake-провайдере для привязки.

## Критерий готовности
Аккаунт с паролем привязывает GitHub из профиля и потом входит через него на сайте.
