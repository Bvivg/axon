# Handoff — состояние работ

> Живой файл: описывает, **где мы сейчас**, а не что решено — решения лежат в
> `roadmap.md` и `stage-<N>-*.md`. Обновляется в конце сессии, чтобы следующая
> (или другой человек) поднималась с нуля, не перечитывая историю.
>
> Последнее обновление: 2026-08-10.

## Где мы

`main` = `745a0b5` — этап 1 слит squash-мержем через
[PR #3](https://github.com/Bvivg/axon/pull/3), все десять проверок CI зелёные.
Веток, кроме `main`, на remote нет; worktree-каталоги параллельных сессий сняты.

| | Статус |
|---|---|
| Этап 0 — фундамент | ✅ закрыт (PR #1, #2) |
| Этап 1 — auth + gateway + веб-логин | ✅ закрыт, все 10 шагов (PR #3) |
| Этап 2 — chat | транспорт (`shared/pkg/kafka`, `shared/pkg/ws`) готов, сервиса нет |
| Этап 3 — game | движок и Tic-Tac-Toe готовы, `session`/`server`/proto нет |
| Этапы 4–7 | не начинались |

Объём: 131 Go-файл (~22 000 строк) и 359 тестов; веб — 27 файлов TS/TSX
(~1 700 строк). Сгенерированный код в счёт не идёт. Модулей в `go.work` четыре:
`shared`, `services/auth`, `services/gateway`, `services/game`.

Тестовое покрытие сквозного пути: 31 Go-сценарий e2e через gateway и 8
браузерных в Chromium.

## Что дальше, по порядку

### 1. Этап 2 — chat

Первая незакрытая работа. Транспорт написан заранее и ждёт первого потребителя;
всё остальное — с нуля: Kafka в `docker-compose.yml`, схема `chat` в Postgres,
сам сервис, WebSocket-протокол поверх `shared/pkg/ws`, Redis pub/sub для fan-out
между инстансами, proto-контракт, веб-клиент. План — `stage-2-chat.md`.

### 2. Этап 3 — game

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

Список модулей для линтера продублирован в трёх местах — `Makefile` (`GO_MODULES`),
`ci.yml` и `docker-compose.tools.yml`. Новый сервис нужно вписать во все три,
иначе он собирается и тестируется, но не линтуется.

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
- **`buf breaking` падает, если на базовой ветке нет ни одного `.proto`** —
  модуль без файлов для него ошибка, а не пустая база. В `ci.yml` поэтому стоит
  отдельный шаг-проверка базы; удалять его нельзя, пока история не гарантирует
  наличие контрактов.
- **Реальных OAuth-ключей нет и не ожидается в репозитории.** Google/GitHub/Apple
  написаны, но в реестр не попадают без полного набора креденшелов; весь флоу
  проверяется fake-провайдером, переход на настоящие ключи — правка `.env`, без
  правок кода.
- **Имя от Apple приходит один раз и не этому сервису.** Apple отдаёт его тому,
  кто принял form_post — то есть веб-клиенту, — и клиент возвращает его в
  `CompleteOAuthRequest.display_name`. Провайдер Apple в Go имени не видит вообще.
  Подставляется, только если сам провайдер имени не дал, и только при создании
  аккаунта.
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
| Сервис chat (этап 2) | `.claude/plans/stage-2-chat.md` |
| Как делили работу между сессиями | `.claude/plans/parallel-sessions.md` |
| Общий порядок этапов | `.claude/plans/roadmap.md` |
| Конвенции (Go, proto, web, infra, тесты, git, security) | `.claude/rules/*.md` |
| Refresh-кука | `core/services/gateway/internal/cookie/` |
| Веб-клиент, auth-слой | `ui/web/src/lib/auth/`, `ui/web/src/lib/connect/` |
| E2E-наборы | `core/services/auth/e2e/`, `ui/web/e2e/` |
