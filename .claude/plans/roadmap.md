# Axon — общий план работ (greenfield roadmap)

> Здесь — порядок этапов и их содержание. Текущее состояние работ (на чём
> остановились, что дальше, чем запускать) — в `handoff.md`.

## Context

Репозиторий сейчас пустой: только `CLAUDE.md`, 9 файлов конвенций в `.claude/rules/`,
`schema.jpg` и IDE-конфиги. Один коммит `init`, в котором лежат исключительно файлы
`.idea/`. Вся структура из «Структура папок» в `CLAUDE.md` — целевая, на диске её нет.

Цель — портфолио-проект: показать умение собрать модульный микросервисный монорепо
на Go с realtime-функциональностью и несколькими клиентами. Отсюда следует, что
важнее не количество игр, а **качество сквозного пути**: аутентификация, граница
доверия на gateway, observability, тесты, воспроизводимый запуск через Docker.

Согласованный порядок (ответы пользователя):
1. **Auth** — password flow + OAuth (Google / GitHub / Apple), ключи провайдеров подставляются позже
2. **Gateway** — параллельно с auth, фактически вместе с ним образует первый рабочий срез
3. **Chat**
4. **Одна игра** — Tic-Tac-Toe как базовая (шахматы — флагман, но позже)
5. **Calling (LiveKit)** — в последнюю очередь

Клиент: **Next.js web** первым, SwiftUI-приложение — отдельным этапом позже.
Стратегия: **walking skeleton** — на каждом этапе есть работающий сквозной путь,
а не набор заглушек.

Инструменты на хосте проверены: go 1.26.5, docker 29.6.2 / compose v5.3.1,
node 20.20.2, swift 6.3.3. `buf` и `protoc` **не установлены** — и не нужны:
по `rules/infra.md` генерация идёт в контейнере.

---

## Этап 0 — Фундамент репозитория ✅ ВЫПОЛНЕН

Закрыт двумя PR: #1 (shared/pkg, Docker-стек, buf, CI) и #2 (правила проекта,
schema-level изоляция Postgres, пароль Redis).

Расхождения с изначальным замыслом:
- `.env.example` живёт в `core/deploy/`, а не в корне репозитория.
- Хост-порты сдвинуты на 55432/56379 — стандартные заняты другими проектами.
- Изоляция схем оказалась строже задуманного: не только схема на сервис, но и
  отдельная login-роль с `search_path`, прибитым к своей схеме, и отозванным
  `CREATE` в `public`.
- `kafka/` и `ws/` в `shared/pkg` не заводились — переехали на этапы 2–3, где
  появится первый потребитель.



**Цель:** `docker compose up` поднимает пустую, но здоровую инфраструктуру; есть
`shared/pkg`, генерация proto и CI.

### Гигиена репозитория
- `schema.jpg` → `docs/schema.jpg` (в `CLAUDE.md` он и так указан там)
- `.gitignore`: `.idea/`, `.DS_Store`, `.env`, `.env.local`, `node_modules/`, `dist/`, `.next/`
- `.idea/` сейчас **закоммичен** вопреки `.gitignore` → `git rm --cached -r .idea`
- Закоммитить `CLAUDE.md`, `.claude/rules/`, `docs/`

### Каркас Go
- `core/go.work` + модули-заглушки: `shared`, `services/{gateway,auth,chat,game,calling}`
  (сервисы создаём по мере надобности, но `go.work` заводим сразу)
- `core/shared/pkg/`:
  - `logger/` — обёртка над `log/slog`, JSON-хендлер, обязательные поля
    `service` / `correlation_id` / `level` / `timestamp`, извлечение correlation_id из `context.Context`
  - `config/` — загрузка из env, fail-fast при отсутствии обязательных значений
  - `postgres/`, `redis/` — конструкторы пулов + `Ping` для `/readyz`
  - `middleware/` — correlation_id (генерация/проброс), recovery, Prometheus-метрики
    (rate/errors/latency по RPC-методу), логирование запросов
  - `health/` — общий хендлер `/healthz` (liveness) и `/readyz` (проверка зависимостей)
- `kafka/` и `ws/` — откладываем до этапов 2–3, когда появится реальный потребитель

### Proto и генерация
- `core/shared/proto/` + `buf.yaml`, `buf.gen.yaml`
- Генерация через контейнер: сервис `buf` в `docker-compose.tools.yml`, запуск
  `docker compose run --rm buf generate` — на хост `buf` не ставим
- Плагины: `protoc-gen-go`, `connect-go`, `connect-es` (TS). `connect-swift` — на этапе SwiftUI

### Docker
- `core/deploy/docker-compose.yml`: `postgres`, `redis` + healthcheck'и;
  `kafka` добавляется на этапе 2, `livekit` — на этапе 4
- `.env.example` с плейсхолдерами (OAuth client id/secret, JWT-ключи, DSN), реальный `.env` — в `.gitignore`
- Dockerfile-шаблон для Go-сервиса (multi-stage, dev-таргет с hot reload)

### CI
- `.github/workflows/ci.yml`: `gofmt` + `golangci-lint` → unit-тесты → build образов.
  Integration/e2e-шаг добавляем, когда появится первый e2e-тест (этап 1)
- Кэш Go-модулей и npm обязателен

**Готово когда:** `docker compose up -d` поднимает postgres+redis здоровыми,
`docker compose run --rm buf generate` отрабатывает, CI зелёный на пустом репо.

---

## Этап 1 — Auth + Gateway + веб-логин ✅ ВЫПОЛНЕН (все 10 шагов)

Закрыт [PR #3](https://github.com/Bvivg/axon/pull/3) — squash-мерж в `main`,
десять проверок CI зелёные, включая оба набора e2e.

Самый большой и самый важный этап. Делаем auth и gateway вместе — по отдельности
ни один из них не проверяем.

Критерий готовности достигнут: в браузере проходят регистрация, вход паролем и
вход через провайдера, профиль читается по access-токену, перезагрузка вход не
роняет — и всё это закодировано в Playwright-наборе, который гоняется в Chromium
внутри сети compose. Детали и принятые решения — в `stage-1-auth-gateway.md`.

Главное расхождение с замыслом: `httpOnly`-кука для refresh-токена оказалась не
частью веб-клиента, а интерцептором на gateway. Контракт возвращает токен в теле,
и превращать его в куку на клиенте значило бы делать это в каждом клиенте заново;
на границе доверия это делается один раз. Веб-клиент из-за этого хранит в
JavaScript только access-токен.

### Контракты (`shared/proto/auth/v1/auth.proto`)
`Register`, `Login`, `RefreshToken`, `Logout`, `GetMe`, `StartOAuth`, `CompleteOAuth`.
Именование — глагол+существительное, все новые поля `optional` (`rules/api-contracts.md`).

### Сервис `auth`
- Миграции в `services/auth/migrations/`, схема `auth`:
  `users`, `credentials` (argon2id-хэш пароля), `refresh_tokens`, `oauth_accounts`
- Пароли — **argon2id**, не bcrypt-«потому что привычнее»
- JWT RS256:
  - JWKS-эндпоинт с **несколькими активными ключами и `kid`** с первого дня (`rules/security.md`),
    даже если ключ пока один — ротация не должна требовать переделки
  - Access — короткоживущий (~15 мин), refresh — в БД, **rotation + reuse detection**
    (использованный повторно refresh инвалидирует всю цепочку)
- OAuth (`internal/oauth/`): интерфейс `Provider` + реализации Google / GitHub / Apple.
  Apple отличается (client_secret — подписанный JWT), это учесть в интерфейсе сразу.
  **Ключи подставляются позже**: пишем код и `.env.example`, для разработки и e2e —
  локальный fake-OAuth-провайдер в compose, чтобы флоу проверялся без реальных креденшелов
- `/healthz`, `/readyz`, `/metrics`

### Сервис `gateway`
- Connect-хендлер: один контракт как gRPC + gRPC-Web + JSON
- JWT-middleware: **локальная верификация через JWKS** с кэшем и фоновым обновлением,
  без похода в auth на каждый запрос
- CORS — явный allow-list origin, никаких `*`
- Rate limiting по `user_id`/IP, отдельные лимиты на дорогие операции
- Входная валидация (форматы, лимиты длины) — здесь, это граница доверия
- correlation_id генерируется на входе и прокидывается в gRPC-метаданных downstream

### Web (`ui/web`)
- Next.js скелет, Tailwind + shadcn + @base-ui
- `lib/connect/` — клиент `@connectrpc/connect-web`, `lib/query/` — TanStack Query хуки
- Роуты `(auth)`: логин, регистрация, OAuth-кнопки, обработка callback
- Хранение токенов + автоматический refresh на 401
- Типы — только сгенерированные из proto, руками не пишем

### Тесты
- Unit: JWT issue/verify, refresh rotation + reuse detection, argon2id, валидация входа
- Integration: `auth` + реальный Postgres (testcontainers-go)
- **E2E (обязателен, `rules/testing.md`):** регистрация → логин → gateway принимает
  токен на защищённом методе → refresh → logout инвалидирует. Отдельный e2e на OAuth
  через fake-провайдер. Всё в `docker-compose.e2e.yml`
- CI: добавить integration/e2e-шаг

**Готово когда:** в браузере можно зарегистрироваться, залогиниться (паролем и через
fake-OAuth), увидеть свой профиль; e2e зелёный в CI.

---

## Этап 2 — Chat

**Статус: транспорт готов, сервиса нет.** `shared/pkg/kafka` и `shared/pkg/ws`
написаны заранее, отдельной параллельной задачей, и покрыты unit- плюс
integration-тестами (Kafka через testcontainers). Это сознательное отступление
от исходного «отложить до первого потребителя»: чтобы не проектировать в пустоту,
пакеты сделаны ровно под двух названных потребителей — fan-out `chat.message` и
события `game.started`/`game.finished`. Детали — в `stage-2-transport.md`.

- ~~`shared/pkg/kafka` (producer/consumer, correlation_id в headers)~~ ✅
- ~~`shared/pkg/ws` — обёртка над `coder/websocket` (пинги, backoff, graceful close)~~ ✅
- Kafka в `docker-compose.yml` — вместе с самим сервисом chat
- Схема `chat` в Postgres: `rooms`, `messages`, `members`. Postgres — источник истины
- Redis pub/sub — fan-out между инстансами chat-сервиса и presence; всё восстановимо из Postgres
- Kafka-событие `chat.message` (для будущих потребителей — уведомления, аналитика)
- WebSocket-протокол — отдельный от Connect, не просовываем ходы/сообщения в unary
- Авторизация уровня ресурса **внутри chat-сервиса**: участник ли этот пользователь комнаты
- Web: `react-use-websocket` с явным backoff, история сообщений через TanStack Query,
  виртуализация длинных списков (TanStack Virtualizer)
- **E2E:** два клиента в одной комнате, сообщение от A доходит до B; переподключение
  после разрыва не теряет историю

**Готово когда:** два браузера обмениваются сообщениями в реальном времени, e2e зелёный.

Виды сообщений, reply/forward и скрытие чата для одного участника — не в этом
этапе. Решения по ним, включая почему это меняет схему `messages` и почему
поиск людей — отдельная процедура в auth, — записаны в `chat-rich-messaging.md`
на случай, если к чату вернутся до этапа 4.

---

## Этап 3 — Game: движок + Tic-Tac-Toe

Инфраструктура realtime уже отлажена на чате — здесь фокус на доменной модели.

**Статус: домен готов, инфраструктуры нет.** Движок и Tic-Tac-Toe написаны
заранее, отдельной параллельной задачей — чистый домен ни от чего не зависит,
поэтому его можно было делать одновременно со всем остальным. `State`/`Move` —
конверт: общий скелет плюс непрозрачный JSON-payload, который читает только сама
игра. Обоснование, почему это переживёт шахматы и Quoridor, — в
`stage-3-game-engine.md`.

- ~~`internal/engine/`: интерфейс `Game` + `registry.go`~~ ✅
- ~~`internal/games/tictactoe/` — первая реализация, `ApplyMove` чистая функция~~ ✅
- `internal/session/`: `Manager` (состояние в памяти + Redis-кэш), `Repository` (Postgres,
  схема `game`: `sessions`, `moves` — полная история ходов, состояние восстановимо реплеем)
- `internal/server/`: `grpc.go` (`CreateSession`, `ListActiveGames`, `GetState`),
  `ws.go` (ходы). Авторизация «может ли этот игрок ходить в этой сессии» — здесь, не на gateway
- Kafka: `game.started`, `game.finished`
- Метрики: активные сессии по типу игры, среднее время партии (для дашборда в портфолио)
- Web: `components/games/tictactoe/` — рендеринг отдельно от транспорта
  (`Board.tsx` рисует, `useGameSocket.ts` — только WS). Клиент правила **не дублирует**,
  подсветка доступных ходов — хинт от сервера
- **E2E:** полная партия от создания сессии до победы и до ничьей

**Готово когда:** два игрока играют партию в браузере, e2e покрывает победу и ничью.

---

## Этап 4 — Chess (флагманская игра)

- `internal/games/chess/` за тем же интерфейсом `Game` — новая игра не требует правок
  инфраструктуры, это и есть демонстрация архитектуры
- Полные правила: рокировка, взятие на проходе, превращение, шах/мат/пат,
  ничья по повторению и правилу 50 ходов
- Обширные table-driven тесты, включая позиции из FEN
- Web: доска, drag&drop, история ходов
- **E2E:** партия до мата + партия до ничьей

**Готово когда:** шахматная партия играется end-to-end; добавление игры не потребовало
изменений в `session`/`server`/gateway — это фиксируем в `docs/architecture.md`.

---

## Этап 5 — Calling (LiveKit)

- `livekit` в compose, сервис `calling` — **тонкий оркестратор**: комнаты, токены, вебхуки.
  Медиапоток идёт клиент ↔ LiveKit напрямую, никогда через gateway
- Схема `calling`: `rooms`, `participants`; Redis — presence участников
- Kafka: `call.started`, `call.ended`
- Web: LiveKit client SDK, звонок поверх игровой сессии
- **E2E:** создание комнаты → выдача токена → участник подключился (медиа мокаем,
  проверяем оркестрацию: комнаты/токены/вебхуки)

---

## Этап 6 — SwiftUI-клиент

Идёт после того, как контракты стабилизировались — переписывать клиент под меняющийся
API дорого.

- Один multiplatform-таргет iOS + macOS, различия через точечный `#if os(macOS)`
- `Core/Networking`: `ConnectClient` (Connect-Swift), `WebSocketClient`, `AuthTokenStore` (**Keychain**, не UserDefaults)
- `Features/`: Auth → Lobby → Games → Calling, `@Observable` view models, во `View` только рендеринг
- Генерация Swift-типов из тех же proto (`connect-swift` в buf-контейнере)
- XCUITest против того же Docker-стека через настраиваемый base URL

---

## Этап 7 — Финальная витрина

- `docs/architecture.md`: диаграмма, решения и обоснования (почему Connect, почему
  schema-level изоляция, почему Redis не источник истины)
- README с гифками/скриншотами и командой запуска в одну строку
- Grafana-дашборд по метрикам (активные сессии, latency) — сильный визуальный аргумент в портфолио
- OpenTelemetry-трейсинг поверх уже заложенного `correlation_id`
- k8s-манифесты в `deploy/k8s/` — по возможности, не блокирующая цель

---

## Ключевые файлы (создаются с нуля)

| Что | Где |
|---|---|
| Workspace Go | `core/go.work` |
| Общие пакеты | `core/shared/pkg/{logger,config,postgres,redis,middleware,health}` |
| Контракты | `core/shared/proto/<service>/v1/*.proto`, `buf.yaml`, `buf.gen.yaml` |
| Инфраструктура | `core/deploy/docker-compose.yml`, `docker-compose.e2e.yml`, `.env.example` |
| Auth | `core/services/auth/internal/{jwt,oauth,repository}`, `migrations/` |
| Gateway | `core/services/gateway/internal/{connect,router}` |
| Web | `ui/web/{app,components,lib}` |
| CI | `.github/workflows/ci.yml` |

## Верификация

Единый принцип из `rules/infra.md` — **всё через Docker**, на хосте только `gofmt`,
линтеры и чистые unit-тесты доменной логики.

- Инфраструктура: `docker compose up -d` → все сервисы `healthy`, `/healthz` и `/readyz` отвечают
- Unit: `go test ./...` — доменная логика без внешних зависимостей
- Integration: `docker compose -f docker-compose.test.yml run --rm <service> go test ./...`
- E2E: `docker compose -f docker-compose.e2e.yml up --abort-on-container-exit` — полный стек,
  тест идёт реальным клиентским путём (Connect/WebSocket)
- Web: Playwright против поднятого стека
- Ручная проверка каждого этапа в браузере перед закрытием этапа

## Конвенции по ходу работ

- Коммиты — Conventional Commits со скоупом сервиса: `feat(auth): add refresh token rotation`
- Ветки `feat/<scope>-<desc>`, мерж в `main` только через PR со squash
- Один коммит — одна логическая единица, рефактор не смешивается с фичей
- `gofmt` + `golangci-lint` перед каждым коммитом

## Открытые вопросы (решаем по ходу)

- Реальные OAuth-ключи Google/GitHub/Apple — подставляются позже; до этого fake-провайдер в dev/e2e
- Kafka vs Redpanda для локальной разработки (Redpanda легче, API совместим) — решаем на этапе 2
- k8s — не блокирующая цель, docker-compose остаётся основным способом запуска
- Объектное хранилище для вложений/войсов в чате (MinIO в compose или внешний
  S3-совместимый) — не решено, см. `chat-rich-messaging.md`
