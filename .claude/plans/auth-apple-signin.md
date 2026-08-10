# Sign in with Apple и отказ в `LinkOauthAccount`

## Контекст

Две несвязанные задачи в модуле `core/services/auth`, оставшиеся от этапа 1
(`stage-1-auth-gateway.md`, раздел OAuth). Каждая закрывается **своим коммитом**:
одна — новая функциональность, вторая — исправление найденного дефекта.

**Apple.** Единственный не реализованный провайдер. Он не влезает в общий
`httpProvider` (см. комментарий в `internal/oauth/provider.go`) по трём причинам:
`client_secret` — это ES256-JWT, подписанный `.p8`-ключом, а не строка из
консоли; профиль приходит внутри `id_token` в ответе на обмен и проверяется по
Apple JWKS; callback — `form_post`, а не редирект с query. Реальных ключей нет
и не будет в этой сессии, поэтому результат — код и unit-тесты, а поведение
деплоя не меняется: реестр закрыт по умолчанию, Apple без всех четырёх
креденшелов остаётся `ErrProviderUnsupported`.

**`LinkOauthAccount`.** `repository/oauth.go:36` защищает identity от переезда к
другому пользователю (`WHERE oauth_accounts.user_id = EXCLUDED.user_id`), но при
отказе UPDATE затрагивает ноль строк, `Exec` возвращает `nil`, и вызывающий
считает, что привязал. Видимое последствие — в `service/oauth_flow.go:181`:
человека пускают в аккаунт по совпадению адреса, привязки не возникает, и на
следующем входе `OauthAccountByProviderID` уводит его в другой аккаунт.
Результат — молчаливый отказ становится явной ошибкой на всех трёх уровнях
(repository → domain → connect.Code).

## Задача 1 — Apple

### Решение по form_post-роуту: отдельный путь в веб-клиенте

Apple присылает callback методом POST (`response_mode=form_post` обязателен,
когда запрошен scope `name email`). Существующая клиентская страница
`/auth/callback/[provider]` читает query и на POST не отвечает, а route handler
по тому же пути перехватил бы и GET, и редирект на самого себя зациклился бы.

Выбран **отдельный серверный роут в вебе**: `POST /auth/apple/callback` →
303 на существующую страницу `/auth/callback/apple?code=…&state=…`.
Соответственно `redirect_uri` Apple — не общий
`OAUTH_REDIRECT_BASE_URL + "/apple"`, а `<origin этого базиса>/auth/apple/callback`,
выводится в реестре, отдельной пятой переменной окружения не заводится.

Отвергнутая альтернатива — принимать form_post **на самом сервисе auth**
(особый `redirect_uri` в конфиге). Она удобнее тем, что не требует веба вообще,
но требует публично доступного эндпоинта на `auth`: POST шлёт браузер, а не
сервер Apple. `rules/security.md` объявляет границей доверия gateway, и `auth`
наружу не смотрит — публиковать ради этого ещё один вход хуже, чем добавить
роут на уже публичном origin веб-клиента.

### Имя пользователя: приходит один раз

Поле `user` (JSON с `name.firstName`/`name.lastName`) Apple присылает **только**
при самой первой авторизации и только в теле form_post. Второго шанса нет:
последующие входы не содержат имени, и `id_token` его не несёт никогда.

Контракт `CompleteOAuth` (`provider`, `code`, `state`) менять нельзя — `shared/proto`
вне границ этой задачи и правится параллельной сессией. Поэтому route handler
упаковывает имя вместе с кодом в **один непрозрачный `code`**:
`base64url(JSON{"code": "<код Apple>", "name": "<имя>"})`. Провайдер Apple в Go
распаковывает его, отдаёт внутренний код в обмен, а имя — в
`ProviderProfile.DisplayName`. Голая строка (без обёртки) принимается как есть,
так что повторные входы и любой другой источник кода работают без изменений.
Формат описан комментарием с обеих сторон и разбирается ровно в двух местах.

Имя — самоописание пользователя и у Apple тоже не проверяется, так что
подделать через обёртку можно ровно то, что и так вводится руками; адрес всегда
берётся из подписанного `id_token`, а `domain.ValidateDisplayName` отбрасывает
негодное значение (существующее поведение `resolveOauthUser`).

### client_secret

ES256-JWT, минтится в процессе и кэшируется: `iss=team_id`, `sub=client_id`,
`aud=https://appleid.apple.com`, `iat/exp`, заголовок `kid=key_id`, `alg=ES256`.
TTL — 30 минут с перевыпуском за 5 минут до истечения (Apple разрешает до 6
месяцев; потолок вынесен в константу и проверяется в конструкторе, чтобы
неудачная правка TTL падала при старте, а не на чужом входе). Ключ `.p8` —
PKCS#8 EC P-256; кривая проверяется явно, ES256 определён только для P-256.

### id_token

Проверяется локально: `alg` пинится в RS256 (`authn.Algorithm`), `kid` → ключ из
Apple JWKS, `iss=https://appleid.apple.com`, `aud=client_id`, `exp` обязателен,
допуск на рассинхрон часов — `authn.Leeway`. Кэш ключей — существующий
`shared/pkg/authn.Cache` (фоновое обновление, дозагрузка по неизвестному `kid` с
кулдауном) — второй экземпляр той же логики разошёлся бы с первым.
`email`/`email_verified`/`is_private_email` читаются из claims; Apple исторически
шлёт булевы значения строками, поэтому принимаются обе формы.

Непроверенный адрес — отказ, через существующий `validateProfile`: аккаунты
сопоставляются по адресу, и без этой проверки чужой аккаунт забирается
регистрацией у ленивого провайдера.

`is_private_email` (адрес вида `@privaterelay.appleid.com`) кладётся в
`domain.ProviderProfile` и попадает в лог входа — иначе непонятно, почему у
человека такой адрес и почему почта до него идёт через релей Apple.

### PKCE

`code_challenge`/`code_verifier` отправляются, как и остальным провайдерам.
Apple их не документирует, но RFC 6749 предписывает игнорировать нераспознанные
параметры, а отключать PKCE у провайдера с самым долгоживущим секретом — худший
из двух рисков. Проверить на живых ключах в этой сессии нечем; если Apple
всё-таки откажет, правка — одна строка в `apple.go`.

### Файлы

| Файл | Что |
|---|---|
| `core/services/auth/internal/oauth/apple.go` | провайдер: authorize URL, обмен, разбор `id_token`, распаковка кода |
| `core/services/auth/internal/oauth/apple_secret.go` | ES256-ассерция: разбор `.p8`, минт, кэш с ротацией |
| `core/services/auth/internal/oauth/registry.go` | `AppleConfig`, регистрация при всех четырёх креденшелах, вывод `redirect_uri` |
| `core/services/auth/internal/config/config.go` | четыре `OAUTH_APPLE_*`, PEM либо base64 (как у JWT-ключей) |
| `core/services/auth/cmd/auth/main.go` | проброс конфига и логгера в реестр |
| `core/services/auth/internal/domain/user.go` | `ProviderProfile.IsPrivateEmail` |
| `core/services/auth/internal/service/oauth_flow.go` | `private_email` в логе входа |
| `ui/web/src/app/auth/apple/callback/route.ts` | приём form_post, упаковка имени, 303 |
| `core/deploy/.env.example` | комментарий к блоку Apple: провайдер реализован, Return URL — `/auth/apple/callback` |

### Чем проверяется без ключей

`apple_test.go`, `apple_secret_test.go` (пакет `oauth`, как `provider_test.go` —
эндпоинты подменяются пакетными переменными), P-256 генерируется прямо в тесте:

- claims и подпись `client_secret`: `iss/sub/aud/exp`, `kid` и `alg` в
  заголовке, проверка подписи публичной половиной, потолок `exp`;
- кэш секрета: тот же токен до окна перевыпуска, новый — после;
- полный обмен против подставного token-эндпоинта и подставного JWKS
  (`authn.Cache` смотрит на `httptest`): профиль собран из `id_token`, запрос
  нёс `client_secret`, `code`, `code_verifier`, `redirect_uri`;
- `email_verified=false` (и строкой `"false"`) — `ErrOauthEmailUnverified`;
- `id_token`, подписанный чужим ключом, с чужим `aud`, с чужим `iss`,
  просроченный, с неизвестным `kid` — отказ, а не профиль;
- обёртка кода: имя достаётся, голый код проходит насквозь;
- authorize URL: `response_mode=form_post`, scope, PKCE, `redirect_uri`;
- реестр: три из четырёх переменных = провайдера нет; все четыре — есть, и
  `redirect_uri` ведёт на `/auth/apple/callback`.

E2E на Apple не заводится: без реальных ключей он проверял бы заглушку.

## Задача 2 — отказ в `LinkOauthAccount` становится видимым

- `repository/oauth.go`: `CommandTag.RowsAffected() == 0` после `ON CONFLICT DO
  UPDATE` означает, что сработала защита, — возвращается
  `domain.ErrOauthIdentityClaimed`. Doc-комментарий метода переписывается: он
  сейчас описывает защиту, но не то, что об отказе не сообщают.
- `domain/errors.go`: sentinel `ErrOauthIdentityClaimed`.
- `service/oauth_flow.go`: отказ вместо входа — человек не пускается в аккаунт,
  к которому привязка не состоялась, и пишется `warn` (это либо гонка, либо
  попытка перехвата).
- `server/errors.go`: `connect.CodeAlreadyExists` с внятным сообщением —
  «эта учётная запись провайдера уже привязана к другому пользователю».
- `service/store_fake_test.go`: in-memory store повторяет новую семантику SQL
  (сейчас он тоже молча возвращает `nil`), плюс хук `beforeLink` — тем же
  приёмом, что `beforeRotate`, гонка воспроизводится детерминированно.

Побочный эффект, который стоит назвать: `CreateUserWithOauthAccount` вызывает
`LinkOauthAccount` внутри транзакции, так что раньше проигравший гонку создавал
пользователя **без** привязки; теперь транзакция откатывается.

Тесты:
- integration на две учётки — **правка существующего теста, а не новый**.
  `TestAProviderIdentityNeverMovesToAnotherUser` (`integration/user_test.go`)
  уже покрывал этот сценарий и прямо фиксировал молчание: «ON CONFLICT update
  пропускается, так что это no-op, а не ошибка». Новый тест рядом оставил бы в
  наборе утверждение, что молчать правильно, поэтому старый теперь ждёт
  `ErrOauthIdentityClaimed` — вместе с сохранённой проверкой, что повторный
  вход того же пользователя проходит и обновляет email;
- integration, новый (`integration/oauth_test.go`): тот же отказ через
  транзакцию `CreateUserWithOauthAccount` — она раньше коммитила пользователя
  **без** привязки, теперь откатывается и не оставляет ни строки;
- unit (`service/oauth_flow_test.go`): вход по совпадению адреса, когда
  identity успела достаться другому аккаунту, отказывает и не выдаёт токенов.

## Как проверяется

```
cd core && gofmt -l .          # пусто
make check                     # fmt + vet + go test -race
make lint                      # golangci-lint, включая теги integration/e2e
make test-integration          # testcontainers, Postgres
COMPOSE_PROJECT_NAME=axon-e2e-b make test-e2e   # весь набор зелёный
make web-lint && make web-typecheck && make web-build   # роут Apple собирается
```

`make up` не запускается: dev-стек занят параллельной сессией.

Расхождение в счёте: набор e2e — это 29 тестов (плюс 7 подтестов внутри них),
а не 31, как сказано в `parallel-sessions.md`. Ни одного файла в `e2e/` эта
работа не трогала, так что расхождение было и до неё.

## Критерий готовности

Два коммита на `feat/auth-apple-signin`: Apple реализован и покрыт unit-тестами,
не меняя поведения деплоя без ключей; отказ в `LinkOauthAccount` виден вызывающему
и покрыт integration-тестом — при зелёных `make check`, `make lint`,
`make test-integration` и прежних e2e.
