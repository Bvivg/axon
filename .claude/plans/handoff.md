# Handoff — состояние работ

> Живой файл: описывает, **где мы сейчас**, а не что решено — решения лежат в
> `roadmap.md` и `stage-<N>-*.md`. Обновляется в конце сессии, чтобы следующая
> (или другой человек) поднималась с нуля, не перечитывая историю.
>
> Последнее обновление: 2026-08-10.

## Где мы

Ветка `feat/auth-gateway`, **32 коммита** поверх `origin/main` (`edb7fc5`),
fast-forward — расхождения нет, force-push не нужен.

| | Статус |
|---|---|
| Этап 0 — фундамент | ✅ закрыт (PR #1, #2) |
| Этап 1 — auth + gateway + веб-логин | ✅ **закрыт, все 10 шагов** |
| Этап 2 — chat | транспорт (`shared/pkg/kafka`, `shared/pkg/ws`) готов, сервиса нет |
| Этап 3 — game | движок и Tic-Tac-Toe готовы, `session`/`server`/proto нет |
| Этапы 4–7 | не начинались |

Объём: 131 Go-файл (~22 000 строк) и 356 тестов; веб — 27 файлов TS/TSX
(~1 700 строк). Сгенерированный код в счёт не идёт. Модулей в `go.work` четыре:
`shared`, `services/auth`, `services/gateway`, `services/game`.

### Последняя сессия: четыре задачи параллельно

Четыре ветки написаны независимыми сессиями в отдельных `git worktree` и сведены
в `feat/auth-gateway` девятью коммитами, cherry-pick, **без конфликтов** —
разделение по файлам сработало. Ветки-worktree ещё существуют и могут быть
удалены: `feat/web-playwright`, `feat/auth-apple-signin`,
`feat/game-engine-tictactoe`, `feat/shared-kafka-ws`. Страховочная ветка на
состояние до сведения — `backup/pre-merge`.

```
c1bcab9 test(shared): cover the Kafka transport against a real broker
86e14b3 feat(shared): add the Kafka producer and consumer
2c6a296 feat(shared): add the WebSocket transport wrapper
1949161 docs(plans): plan the shared transport for stage 2
77703e1 fix(auth): report a refused oauth link instead of silent success
e4d7aa5 feat(auth): add sign in with Apple
65d23ea ci: run the browser e2e suite
1715c3c test(web): drive the browser sign-in flow with Playwright
956336a feat(game): add the engine contract and tic-tac-toe behind it
```

Всё проверено **на сведённой ветке**, а не только по отдельности: `make check`
(0 FAIL), `make lint` (0 issues), `make test-integration` (auth + shared),
`make test-e2e` (29 сценариев), `make test-web-e2e` (6 сценариев в браузере),
`make web-check` — все `exit=0`.

## Что дальше, по порядку

### 1. PR по этапу 1

`main` защищена, мерж только через PR со squash (`rules/git-ci.md`). Ветка ещё
не запушена (`origin/feat/auth-gateway` не существует). **Пуш делает пользователь
сам** — автоматический режим его блокирует:

```bash
git push -u origin feat/auth-gateway
```

### 2. Имя Apple — отдельным полем в контракте

Apple присылает имя пользователя ровно один раз, при первой авторизации, а
`CompleteOAuth` несёт только provider/code/state. Сейчас имя упаковано вместе с
кодом в одно поле: `base64url(JSON{code,name})` — работает и документировано с
обеих сторон, но это протокол поверх поля, не предназначенного для этого.
Правильное место — отдельное `optional`-поле в `auth.proto`; правило
additive-only это позволяет сделать чисто.

### 3. Этап 2 — chat

Транспорт уже есть и ждёт первого потребителя. Осталось: Kafka в
`docker-compose.yml`, схема `chat` в Postgres, сам сервис, WebSocket-протокол
поверх `shared/pkg/ws`, Redis pub/sub для fan-out между инстансами, веб-клиент.

### 4. Этап 3 — game

Домен готов и от инфраструктуры не зависит. Осталось: `session` (Manager +
Repository, история ходов в Postgres), `server` (gRPC + ws), proto-контракт,
сервис в compose, регистрация игр при wiring.

## Как всё запускается — только через Docker

`rules/infra.md`: на хосте не выполняется ничего, кроме `gofmt`, линтеров и
чистых unit-тестов. Всё остальное — через `make`, который ходит в compose.

| Команда | Что делает |
|---|---|
| `make env` | `.env` из `.env.example` — без него compose не стартует |
| `make proto` | генерация Go + TS; **обязательна первой** после чистого клона |
| `make web-install` | `npm ci`; нужна после любой правки `package.json` |
| `make up` / `make down` / `make logs` | dev-стек: postgres, redis, auth, gateway, web |
| `make check` | gofmt + vet + `go test -race` |
| `make lint` | golangci-lint, включая код за тегами `e2e`/`integration` |
| `make test-integration` | testcontainers: Postgres для auth, Kafka для shared |
| `make test-e2e` | полный стек на **runtime**-образах, сносится после прогона |
| `make test-web-e2e` | Playwright в Chromium внутри сети compose |
| `make web-check` | lint + typecheck + build веб-клиента через сервис `node` |

Порты на хосте: web 3000, gateway 18080 (публичный) и 19090 (админский),
postgres 55432, redis 56379. Стандартные заняты другими проектами.

Имена compose-проектов глобальны (`axon`, `axon-e2e`, `axon-web-e2e`,
`axon-tools`), поэтому **два стека одновременно держать нельзя**: параллельные
сессии либо не трогают Docker, либо переопределяют `COMPOSE_PROJECT_NAME`.

## Что дорого переоткрывать заново

- **Куку ставит gateway, не веб.** Интерцептор `services/gateway/internal/cookie`.
  Браузер определяется по наличию заголовка `Origin` — подделать со стороны
  страницы нельзя, поэтому «прикинуться не-браузером и получить токен в теле» не
  сценарий. Клиенты без `Origin` (будущий connect-swift, Go-набор e2e) не задеты.
- **Веб-клиент и gateway обязаны делить общий регистрируемый домен.** Иначе
  `SameSite=Lax` режет refresh-куку, и выглядит это не как отказ, а как сломанный
  код сессии: вход проходит, перезагрузка разлогинивает. Chromium сообщает
  причину только через CDP. В dev работает случайно — оба на `localhost`; в
  браузерном e2e пришлось дать контейнерам имена под общим `axon.test`.
  Однословные `web` и `gateway` — два разных сайта.
- **`.test`, а не `.dev`** для таких имён: `.dev` в HSTS-preload, Chrome
  принудительно переводит его на HTTPS и падает на plaintext-сети.
- **E2E не использует `cookiejar`** и держит куку руками: e2e-gateway работает с
  `APP_ENV=test`, то есть `Secure=true`, а jar отказался бы слать такую куку по
  plaintext-сети compose. По той же причине браузерный стек ставит
  `REFRESH_COOKIE_SECURE=false` явно.
- **`ui/web/src/gen` в `.gitignore`.** Без `buf generate` веб не собирается, и
  падает невнятно — на отсутствующих типах. Поэтому шаг генерации есть и в
  образе, и в каждом джобе CI.
- **`NEXT_PUBLIC_*` инлайнится в бандл на сборке**, то есть значение читает
  браузер, а не контейнер. Для dev-стека `NEXT_PUBLIC_API_BASE_URL` — адрес, по
  которому до gateway дотягивается хост; в браузерном e2e браузер живёт внутри
  сети compose, поэтому там это внутреннее имя, переданное build-arg'ом.
- **Реальных OAuth-ключей нет и не ожидается в репозитории.** Google/GitHub/Apple
  написаны, но в реестр не попадают без полного набора креденшелов; весь флоу
  проверяется fake-провайдером, переход на настоящие ключи — правка `.env`, без
  правок кода.
- **Реестр игр заполняется явно при wiring** (`reg.Register(tictactoe.Definition())`),
  без `init()`-побочек — будущий `cmd/main.go` игрового сервиса обязан это сделать.

## Куда смотреть

| Что | Где |
|---|---|
| Решения по этапу 1 целиком | `.claude/plans/stage-1-auth-gateway.md` |
| Браузерный e2e и его находки | `.claude/plans/stage-1-web-e2e.md` |
| Apple Sign In | `.claude/plans/auth-apple-signin.md` |
| Движок игр | `.claude/plans/stage-3-game-engine.md` |
| Транспорт под этап 2 | `.claude/plans/stage-2-transport.md` |
| Как делили работу между сессиями | `.claude/plans/parallel-sessions.md` |
| Общий порядок этапов | `.claude/plans/roadmap.md` |
| Конвенции (Go, proto, web, infra, тесты, git, security) | `.claude/rules/*.md` |
| Refresh-кука | `core/services/gateway/internal/cookie/` |
| Веб-клиент, auth-слой | `ui/web/src/lib/auth/`, `ui/web/src/lib/connect/` |
| E2E-наборы | `core/services/auth/e2e/`, `ui/web/e2e/` |
