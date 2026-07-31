# Этап 1 — Auth + Gateway + веб-логин

## Context

Этап 0 закрыт: есть `shared/pkg`, Docker-стек, buf и CI, но **ни одного сервиса
и ни одного `.proto`**. Ничего не запускается, кроме Postgres и Redis.

Задача этапа — первый сквозной путь: человек открывает браузер, регистрируется,
логинится, и gateway пускает его на защищённый метод по выданному токену. Это
walking skeleton: после него добавление сервиса становится рутиной, потому что
все неочевидные куски — контракты, генерация, DI, middleware, миграции, e2e в
Docker — уже отлажены на живом примере.

Auth и gateway делаются **вместе**: по отдельности проверить нечего. Auth без
gateway — сервис, к которому никто не ходит; gateway без auth — прокси в пустоту.

Порядок внутри этапа выбран так, чтобы как можно раньше получить работающий
`curl`, а браузерный UI навесить последним.

## Что уже есть и переиспользуется

Ничего из перечисленного заново не пишется:

| Нужно | Готовое |
|---|---|
| Конфиг из env | `config.NewLoader()`, `config.LoadBase(l, service)` → `Base{Service, Environment, HTTPAddr, MetricsAddr, LogLevel, LogFormat, ShutdownTimeout}` |
| Секреты без утечки в логи | `config.Secret` (`NewSecret`/`Reveal`, редактируется в fmt/JSON/slog) |
| Логи | `logger.New(logger.Options{...})` |
| Пробы | `health.New(log, []health.Checker)`, `Register(mux)` |
| Пул Postgres | `postgres.LoadConfig(l)`, `postgres.Connect(ctx, cfg, log)` → `*Pool`, уже `health.Checker` |
| Redis | `redis.LoadConfig(l)`, `redis.Connect(ctx, cfg, log)` → `*Client`, уже `health.Checker` |
| Интерцепторы Connect | `middleware.NewCorrelationInterceptor/NewRecoveryInterceptor/NewLoggingInterceptor` |
| HTTP-middleware | `middleware.Chain/Correlation/Recovery/RequestLogger` |
| Метрики | `middleware.NewMetrics(service)` → `.Interceptor()`, `.Handler()` |
| Сквозной ID | `correlation.Header`, `NewID`, `WithID`, `FromContext`, `Ensure` |

Переменные окружения под auth уже заложены в `core/deploy/.env.example`:
`JWT_ACTIVE_KEY_ID`, `JWT_PRIVATE_KEY_DEV_1`, `JWT_ACCESS_TOKEN_TTL`,
`JWT_REFRESH_TOKEN_TTL`, `JWT_ISSUER`, `OAUTH_FAKE_ENABLED`, `OAUTH_*`,
`CORS_ALLOWED_ORIGINS`, `RATE_LIMIT_PER_MINUTE`, `RATE_LIMIT_AUTH_PER_MINUTE`.
Роль `auth_service` и схема `auth` создаются init-скриптом Postgres, DSN —
`AUTH_POSTGRES_DSN`.

## Баг, который чинится первым

`core/deploy/Dockerfile.service` объявляет `ARG GO_VERSION=1.24`, а `core/go.work`
и `core/shared/go.mod` требуют `go 1.25.0`. Сейчас не проявляется — собирать
нечего. Как только появится `services/auth`, сборка упадёт на `go mod download`.
Одна строка, первый коммит этапа.

---

## 1. Контракты

`core/shared/proto/auth/v1/auth.proto`, пакет `axon.auth.v1`.

| RPC | Назначение |
|---|---|
| `Register` | email + пароль → пара токенов |
| `Login` | email + пароль → пара токенов |
| `RefreshToken` | refresh → новая пара со сменой refresh |
| `Logout` | инвалидация цепочки refresh |
| `GetMe` | профиль по access-токену |
| `StartOAuth` | provider → URL авторизации + `state` |
| `CompleteOAuth` | provider + code + state → пара токенов |

Решения по форме:

- Отдельное сообщение `TokenPair { access_token, refresh_token, expires_in, token_type }`,
  чтобы клиент не гадал про TTL.
- `GetMe` **не принимает** `user_id`: пользователь берётся из токена. Метод,
  принимающий чужой ID, рано или поздно отдаст чужие данные.
- Все новые поля `optional`, номера не переиспользуются (`reserved` при удалении).

Генерация уже настроена: Go уходит в `core/shared/gen/go`, TS — в `ui/web/src/gen`
(`buf.gen.yaml`, managed mode). Проверка — `make proto-lint`, затем `make proto`.

## 2. Сервис `auth`

Новый модуль `core/services/auth` (добавить в `core/go.work` и в `GO_MODULES`
Makefile — сейчас там только `./shared/...`).

```
services/auth/
├── cmd/auth/main.go          # только wiring
├── migrations/
├── .air.toml                 # Dockerfile.service ожидает его в корне модуля
└── internal/
    ├── config/               # Base + специфика auth
    ├── domain/               # User, Credential, RefreshToken, sentinel-ошибки
    ├── repository/           # Postgres, схема auth
    ├── jwt/                  # RS256, JWKS, kid
    ├── password/             # argon2id
    ├── oauth/                # Provider + google/github/apple + fake
    ├── service/              # бизнес-логика, транспорт не знает
    └── server/               # Connect-хендлер, домен-ошибки → connect.Code*
```

### Схема БД (`migrations/0001_init.up.sql`)

| Таблица | Ключевое |
|---|---|
| `users` | `id uuid pk`, `email citext unique`, `email_verified`, timestamps |
| `credentials` | `user_id fk`, `password_hash` — отдельно от `users`, потому что OAuth-пользователь пароля не имеет вовсе |
| `refresh_tokens` | `id`, `user_id`, `token_hash`, `family_id`, `used_at`, `revoked_at`, `expires_at` |
| `oauth_accounts` | `provider`, `provider_user_id`, `user_id`, unique на `(provider, provider_user_id)` |

Сам refresh-токен в БД **не хранится** — только хэш. Утечка дампа не должна давать
возможность войти.

### Пароли

argon2id (`golang.org/x/crypto/argon2`), параметры в конфиге, в хэш пишется строка
с параметрами — иначе их нельзя поднять, не разлогинив всех. Сравнение —
`subtle.ConstantTimeCompare`.

### JWT

- RS256, приватные ключи из env, активный — `JWT_ACTIVE_KEY_ID`.
- **JWKS с первого дня отдаёт множество ключей**, а не один. Ротация: ключ сперва
  появляется в JWKS и только следующим релизом начинает подписывать. Иначе клиенты
  с закэшированным JWKS отвалятся в момент ротации.
- Claims: `sub`, `iss`, `aud`, `exp`, `iat`, `jti`.

### Refresh rotation + reuse detection

Каждое обновление выдаёт новый refresh и помечает старый использованным. Повторное
предъявление использованного токена означает кражу — отзывается **вся цепочка
`family_id`**, а не только предъявленный токен. Обязательный сценарий в тестах.

### OAuth

Интерфейс `Provider`: `AuthURL(state)`, `Exchange(ctx, code)`, `UserInfo(ctx, token)`.
Apple ломает наивный интерфейс — у него `client_secret` это короткоживущий ES256
JWT, подписанный `.p8`-ключом, поэтому секрет добывается методом провайдера, а не
читается из поля конфига.

`state` — одноразовый, в Redis с TTL. Без этого CSRF на callback открыт.

Ключей провайдеров нет → в dev и e2e работает `fake`-провайдер
(`OAUTH_FAKE_ENABLED`), проходящий полный authorization code flow локально. Он
**обязан быть выключен в production** — проверка через `Environment.IsProduction()`.

## 3. Сервис `gateway` — ✅ ВЫПОЛНЕНО

Новый модуль `core/services/gateway`.

- Connect-хендлер отдаёт один контракт как gRPC + gRPC-Web + JSON.
- **JWT проверяется локально** через JWKS с кэшем и фоновым обновлением. Поход в
  auth на каждый запрос — ровно то, чего схема с JWKS должна избежать.
- CORS — явный allow-list из `CORS_ALLOWED_ORIGINS`, `*` недопустим.
- Rate limiting: общий лимит и отдельный жёсткий на `Login`/`Register` — это точки
  для брутфорса.
- Входная валидация (форматы, длины) здесь: gateway — граница доверия.
- correlation_id генерируется на входе и уходит в gRPC-метаданных downstream.

Оба сервиса: `/healthz`, `/readyz`, `/metrics` на отдельном порту (`MetricsAddr`),
не на публичном.

Расхождения с замыслом:

- **Входная валидация осталась в домене `auth`, а не на gateway.** Дублировать
  правила формата email и длины пароля в двух местах — это два места, которые
  разъедутся. Gateway остаётся границей доверия для того, что домену не видно:
  кто звонит (JWT), как часто (rate limit) и откуда (CORS). Когда появятся
  сервисы, которым нужна одинаковая проверка размера тела запроса, она ляжет
  сюда как middleware.
- **Проверка токена вынесена в `shared/pkg/authn`.** Изначально верификатор был
  написан внутри `auth`; gateway'ю понадобился тот же код, и второй экземпляр
  той же логики — гарантированное расхождение при первой же правке.
- **Публичный порт — 18080, админский 19090.** 8080 на машине занят, а высокий
  диапазон здесь и так конвенция (55432/56379).
- **Таблица политик закрыта по умолчанию** (`policy.For` на незнакомый метод
  возвращает «требует токена»): новый RPC, который забыли внести в таблицу,
  ломается заметно, а не открывается молча.

## 4. Web (`ui/web`)

Next.js + Tailwind + shadcn/@base-ui + TanStack Query.

- `lib/connect/` — клиент `@connectrpc/connect-web` на сгенерированных типах,
  ручных `fetch` к gateway нет.
- `app/(auth)/`: логин, регистрация, кнопки провайдеров, обработка callback.
- Access-токен — в памяти, refresh — в `httpOnly`-cookie. Access в `localStorage`
  это XSS-подарок.
- Интерцептор: на 401 один раз пробует refresh и повторяет запрос.

## 5. Тесты — ✅ ВЫПОЛНЕНО (кроме OAuth-сценария)

| Уровень | Что покрывает | Где |
|---|---|---|
| Unit | argon2id (в т.ч. что два хэша одного пароля различаются), JWT issue/verify, выбор ключа по `kid`, валидация входа, reuse detection на моках | рядом с кодом |
| Integration | гонка восьми горутин за один refresh, откат регистрации при конфликте email, границы отзыва семьи, атомарность смены пароля, изоляция схем | `services/auth/integration/`, тег `integration` |
| E2E | 15 сценариев: регистрация → логин → GetMe → refresh → logout, повтор потраченного refresh, отсутствие user-enumeration, rate limit, CORS, админский порт не публичен | `services/auth/e2e/`, тег `e2e` |
| E2E | OAuth через fake-провайдер | **не сделано** — идёт вместе с самим провайдером |

Разделение уровней: в integration попадает только то, что невидимо ни снизу
(моки не арбитрируют гонку), ни сверху (через HTTP тайминг не воспроизвести).

Ключевые решения:

- **E2E гоняется на `runtime`-образах**, а не на `dev` с air. Зелёный прогон
  против контейнера с hot-reload ничего не говорит про то, что уедет в прод.
- **Порты на хост не публикуются.** Прогон не конфликтует ни с dev-стеком, ни с
  другим прогоном, ни с занятым 8080.
- **`TRUSTED_PROXIES=1` на e2e-gateway.** Весь трафик прогона идёт с одного
  адреса, поэтому каждый тест подставляет свой `X-Forwarded-For` и получает
  собственную корзину лимитера. Побочно покрывается путь `ratelimit.ClientIP`,
  которым gateway пойдёт за ingress.
- **Ключ подписи генерируется на каждый прогон** в `core/deploy/.env.e2e`
  (в `.gitignore`) — в репозитории нет ключа даже тестового.
- **Бинарник умеет `-healthcheck`** (`shared/pkg/health`): в distroless-образе
  нет shell, wget и curl, а healthcheck нужен.

В CI стало семь джобов: формат/vet/unit, линтер, контракты, compose,
integration, e2e (с выгрузкой логов сервисов при падении), сборка
runtime-образов. Найденный по дороге баг: линтер в CI не покрывал `gateway`
вообще.

## Порядок работ

1. ✅ Починить `GO_VERSION` в `Dockerfile.service`
2. ✅ `auth.proto` + генерация, убедиться, что код появился
3. ✅ Модуль `auth`: домен, миграции, репозиторий, argon2id, JWT/JWKS
4. ✅ Connect-хендлер auth + `main.go` + сервис в compose, поднять и дёрнуть `curl`
5. ✅ Модуль `gateway`: проксирование, JWT-middleware, CORS, rate limit
6. ✅ E2E на password-флоу (плюс integration-уровень, которого не было в исходном порядке)
7. OAuth: интерфейс, fake-провайдер, google/github/apple
8. E2E на OAuth
9. Web: скелет, connect-клиент, страницы логина и регистрации
10. Playwright на браузерный флоу

Пункты 1–6 дают работающий `curl`-путь, всё после — расширение.

## Верификация

```bash
make up                 # postgres, redis, auth, gateway — все healthy
make proto              # контракты сгенерированы
make check              # gofmt, vet, go test -race
make lint               # golangci-lint, включая e2e/integration за тегами
make test-integration   # testcontainers поднимает Postgres
make test-e2e           # полный стек на runtime-образах, сносится после прогона
```

Ручная проверка: зарегистрироваться и залогиниться в браузере на
`http://localhost:3000`, увидеть профиль. В логах обоих сервисов на один запрос —
один и тот же `correlation_id`.

## Критерий готовности

В браузере проходит регистрация и вход — паролем и через fake-OAuth, — gateway
пускает по выданному токену на защищённый метод, а e2e-сценарии password и OAuth
зелёные в CI.

## Открытый вопрос

Реальные ключи Google/GitHub/Apple вы подставите позже. До этого весь OAuth-флоу
проверяется fake-провайдером; переход на настоящие ключи — правка `.env`, без
изменений в коде.
