# Четыре параллельные сессии: Playwright, Apple, движок игр, транспорт


## Context

Этап 1 закрыт на 9 шагов из 10, работа шла последовательно одной сессией. Дальше
идут четыре задачи, которые не связаны между собой ни доменом, ни файлами, и
держать их в одной очереди — тратить время впустую. Цель этого плана: раздать их
четырём независимым сессиям так, чтобы они не мешали друг другу **физически** и
чтобы результаты сливались без разбора конфликтов.

Главное, что здесь проектируется, — не сами задачи (они понятны), а **границы**:
кто какие файлы правит, кто держит Docker, и что делать с общими файлами, к
которым тянутся сразу трое.

Результат: четыре ветки, каждая с зелёными проверками, сливаемые в любом порядке.

## Почему изоляция обязательна, а не желательна

Две сессии в одном каталоге — это одна файловая система и один git-индекс: они
перезаписывают правки друг друга и коммитят чужое. Поэтому каждая сессия живёт в
своём `git worktree` со своей веткой от одного базиса.

Второй, менее очевидный слой — Docker. Имена compose-проектов в репозитории
заданы явно (`name: axon`, `axon-e2e`, `axon-tools`), а значит они **глобальные**:
worktree их не изолирует. Отсюда правила ниже.

| Ресурс | Кто держит | Почему безопасно / нет |
|---|---|---|
| `axon` (dev-стек, порты 3000/18080/55432/…) | **только A** | Один набор имён контейнеров и хостовых портов на машину |
| `axon-e2e` (`make test-e2e`) | любой, но под своим именем | Хостовые порты не публикуются вовсе, поэтому параллельные прогоны возможны — но имя проекта надо переопределить: `COMPOSE_PROJECT_NAME=axon-e2e-<буква> make test-e2e`. `COMPOSE_PROJECT_NAME` старше `name:` в файле |
| `axon-tools` (`make lint`, `proto`, `web-*`) | все | Это `run --rm`: каждый вызов — свой одноразовый контейнер. Общие тома кэшей (go-mod, npm) переживают конкуренцию |
| testcontainers (`make test-integration`) | все | Свои контейнеры со случайными портами |

## Общий базис

Все четыре ветки растут от коммита, которым добавлен этот файл, — то есть от
`feat/auth-gateway` на момент раздачи задач. Worktree созданы заранее, сессия
стартует уже внутри своего каталога:

```bash
cd /Users/bvivg/Developer/PetProjects/axon
git worktree add -b feat/web-playwright        ../axon-playwright feat/auth-gateway
git worktree add -b feat/auth-apple-signin     ../axon-apple      feat/auth-gateway
git worktree add -b feat/game-engine-tictactoe ../axon-game       feat/auth-gateway
git worktree add -b feat/shared-kafka-ws       ../axon-transport  feat/auth-gateway
```

Свежий worktree — это чистый checkout: в нём **нет** `ui/web/node_modules`,
`ui/web/src/gen` и `core/shared/gen` (всё git-ignored). Сессиям, которым нужен Go
с генерённым кодом или веб, придётся начать с `make proto` (и `make web-install`).

## Кто чем владеет

Правило одно: **файл правит ровно одна сессия**. Всё, что ниже не помечено, не
трогает никто.

| Файл / каталог | A (Playwright) | B (Apple) | C (движок) | D (транспорт) |
|---|:--:|:--:|:--:|:--:|
| `ui/web/e2e/**`, `playwright.config.ts`, `ui/web/package.json` | ✅ | | | |
| `ui/web/src/app/auth/apple/**` (новый роут) | | ✅ | | |
| остальной `ui/web/src/**` | только чтение | | | |
| `core/deploy/docker-compose.web-e2e.yml` (новый), `Dockerfile.web` | ✅ | | | |
| `core/services/auth/**`, `core/deploy/.env.example` | | ✅ | | |
| `core/services/game/**` (новый модуль), `core/go.work` | | | ✅ | |
| `core/shared/pkg/{kafka,ws}/**`, `core/shared/integration/**` | | | | ✅ |
| `.github/workflows/ci.yml` | ✅ | | | |
| `Makefile` | ✅ низ файла | | ✅ строка 14 + `tidy` | ✅ `test-integration` |
| `.claude/plans/<своё имя>.md` | ✅ | ✅ | ✅ | ✅ |
| `roadmap.md`, `handoff.md`, `stage-1-auth-gateway.md` | ❌ никто — обновляет интегратор при мерже |

`Makefile` — единственный файл на троих. Правки лежат в разных секциях (строка
14, `test-integration` на 181, самый низ файла), это разные хунки, и git сольёт
их сам. Чтобы так и осталось, A дописывает свою цель **в самый конец файла**, а
не рядом с `web-check`: там в трёх строках ниже живёт `tidy`, который правит C.

`docker-compose.yml` (dev-стек) не трогает никто. Kafka в него добавит этап 2,
когда появится сервис, которому она нужна.

## Задача A — Playwright, шаг 10 этапа 1

Закрывает этап 1. Флоу уже подтверждён живым браузером вручную, так что тест
кодирует известно-рабочий путь.

Отдельный `core/deploy/docker-compose.web-e2e.yml` (`name: axon-web-e2e`): та же
цепочка postgres → migrate → auth → gateway, что в `docker-compose.e2e.yml`, плюс
`web` и раннер Playwright. Хостовые порты не публикуются — браузер живёт внутри
сети compose.

**Три ловушки, каждая из которых стоит часа отладки:**

1. **`Secure`-кука не доедет.** При `APP_ENV=test` gateway ставит refresh-куку с
   `Secure`, а браузер отвергает такие куки на plaintext-origin, если это не
   `localhost`. `http://web:3000` — не localhost. Нужен явный
   `REFRESH_COOKIE_SECURE=false` в этом стеке (`APP_ENV` при этом можно оставить
   `test`: конфиг читает флаг отдельно). Go-набор e2e этого не ловит, потому что
   он не браузер и куки носит руками.
2. **`NEXT_PUBLIC_API_BASE_URL` вшивается в бандл на сборке** и должен быть
   адресом, по которому до gateway дотягивается **браузер внутри раннера**, то
   есть `http://gateway:8080`. Отсюда же следует, что в `runtime`-таргете его
   надо прокинуть build-arg'ом в `Dockerfile.web` — иначе останется значение
   времени сборки образа. Альтернатива — гонять `dev`-таргет, где переменная
   читается в рантайме, но тогда прогон ничего не говорит о том, что уедет в
   прод; для Go-набора этот вопрос решён в пользу runtime.
3. **Всё остальное окружение должно сойтись на одном origin:**
   `CORS_ALLOWED_ORIGINS`, `OAUTH_REDIRECT_BASE_URL` и
   `OAUTH_ALLOWED_RETURN_ORIGINS` — это `http://web:3000`, а
   `OAUTH_FAKE_AUTHORIZE_URL` — `http://auth:8081/oauth/fake/authorize`, потому
   что по нему идёт браузер, а не сервис.

Полезное про fake-провайдера: его authorize — обычный 302 без HTML-формы
(`internal/oauth/fake.go`), личность зашита на стороне сервера. Значит сценарий —
клик по кнопке и ожидание профиля, кликать в чужом UI не придётся.

Сценарии: регистрация → профиль; вход паролем; вход через fake-провайдера
целиком; перезагрузка не разлогинивает; `document.cookie` refresh-токена не
содержит; выход делает профиль недоступным.

## Задача B — Apple Sign In и отказ в LinkOauthAccount

Два независимых коммита в одной сессии, потому что оба живут в модуле `auth`.

**Apple.** Реестр провайдеров закрыт по умолчанию, поэтому без ключей поведение
не меняется — Apple остаётся `ErrProviderUnsupported`. Отличий от Google/GitHub
три, и они не позволяют переиспользовать `httpProvider`: `client_secret` — это
ES256-JWT, подписанный `.p8` (iss=team, sub=client, aud=appleid.apple.com, с
ротацией по exp), профиль приходит внутри `id_token` и проверяется по Apple JWKS,
а callback — `form_post`, то есть POST в редирект. Имя пользователя приходит
только при первой авторизации отдельным полем `user`, и второго шанса его
получить не будет.

Ловушка веб-роута: `form_post` не может прийти на существующую клиентскую
страницу `/auth/callback/[provider]` — она читает query. Нужен route handler, но
если положить его по тому же пути `/auth/callback/apple`, он перехватит и GET, и
редирект на самого себя зациклится. Значит либо отдельный путь
(`/auth/apple/callback` → 303 на существующую страницу с code/state в query), либо
особый `redirect_uri` для Apple в конфиге auth. Решение — за сессией, но ловушку
надо знать заранее.

Ключей нет, поэтому проверяемость — unit-уровень на локально сгенерированном
P-256: claims и подпись client_secret, разбор `id_token` против подставного JWKS,
отказ при `email_verified=false`. E2E на Apple не появляется.

**Отказ в LinkOauthAccount.** `repository/oauth.go:36` защищает identity от
переезда к другому пользователю через `WHERE oauth_accounts.user_id =
EXCLUDED.user_id`. Защита работает, а вот отчёт — нет: при отказе UPDATE
затрагивает ноль строк, `Exec` возвращает `nil`, и вызывающий считает, что
привязал. Последствие видно в `service/oauth_flow.go:181`: человека пускают в
аккаунт по адресу, привязки при этом не возникает, и на следующем входе
`OauthAccountByProviderID` уводит его в **другой** аккаунт. Починка — проверить
`RowsAffected()`, вернуть sentinel и явно решить на уровне сервиса, что делать
(отказ с внятным сообщением), плюс integration-тест на две учётки.

## Задача C — движок игр и Tic-Tac-Toe (чистый домен)

Новый модуль `core/services/game`, но только доменная часть: `internal/engine`
(интерфейс `Game` + `registry.go`) и `internal/games/tictactoe`. Ни `session`, ни
`server`, ни proto, ни compose, ни Dockerfile — всё это остаётся этапу 3, когда
понадобится инфраструктура. Отсюда же следует, что задача не трогает Docker
вообще: чистая доменная логика — единственное, что `rules/infra.md` разрешает
гонять на хосте.

Интерфейс зафиксирован в `CLAUDE.md` и не обсуждается:

```go
type Game interface {
    Init(players []PlayerID) State
    ApplyMove(state State, move Move) (State, error)
    ValidMoves(state State, player PlayerID) []Move
    IsTerminal(state State) (bool, *PlayerID)
}
```

Настоящая работа — выбрать представление `State`/`Move`. Ограничения, которые
делают этот выбор нетривиальным: он должен пережить сериализацию (этап 3 хранит
историю ходов в Postgres и восстанавливает состояние реплеем), уехать в proto
позже, и вместить шахматы с Quoridor, не ломая `registry.go`. Решение фиксируется
в плане с обоснованием.

`ApplyMove` — чистая функция: без I/O и без мутации входного состояния. Второе
проверяется тестом, а не обещанием.

## Задача D — `shared/pkg/kafka` и `shared/pkg/ws`

Заготовка транспорта под этап 2. Оговорка, которую надо помнить: roadmap отложил
эти пакеты «до появления реального потребителя» намеренно, и мы идём против этого
решения — значит риск в том, что интерфейс окажется удобен воображаемому
потребителю, а не chat'у. Смягчение: сессия начинает с того, что называет в плане
двух конкретных первых потребителей (`chat.message` fan-out и
`game.started`/`game.finished`) и проектирует ровно под них, а не «вообще».

Границы: `core/shared/pkg/kafka`, `core/shared/pkg/ws`, интеграционные тесты в
`core/shared/integration/`. Ни сервиса chat, ни proto, ни kafka в
`docker-compose.yml` — брокер для тестов поднимает testcontainers.

Обязательное к переиспользованию: `correlation_id` уже реализован в
`shared/pkg/correlation` (`Header`, `WithID`, `FromContext`, `Ensure`) — он должен
уезжать в заголовки Kafka и подниматься обратно у консьюмера, иначе трассировка
через шину рвётся, а `rules/observability.md` требует обратного. Консьюмер-группы
— по сервису-потребителю, темы — `<domain>.<event>`.

`ws` — обёртка над `coder/websocket`: пинги, backoff при переподключении,
graceful close с кодами. Никакого знания о чате и играх.

## Верификация

Каждая сессия отвечает за свои проверки; общее — `make check` и `make lint`
зелёные перед каждым коммитом.

| Сессия | Команды |
|---|---|
| A | `make proto && make web-install`, `make web-check`, `make test-web-e2e` (новая цель), плюс глазами `make up` |
| B | `make check`, `make lint`, `make test-integration`, `COMPOSE_PROJECT_NAME=axon-e2e-b make test-e2e` (31 сценарий должен остаться зелёным) |
| C | `make proto` (модулю нужен собранный workspace), `cd core && go test -race ./services/game/...`, `make lint` |
| D | `make check`, `make lint`, `make test-integration` (расширенная цель) |

Интеграция после: слить ветки в любом порядке, `Makefile` разрешить (правки
аддитивные, в разных секциях), затем один прогон `make check && make lint &&
make test-e2e && make test-web-e2e` на объединённом дереве. `roadmap.md` и
`handoff.md` обновляет интегратор — сессии их не трогают именно поэтому.

## Критерий готовности

Четыре ветки, на каждой свои проверки зелёные и свой файл плана в
`.claude/plans/`; после слияния в одно дерево полный набор проверок проходит без
правок, кроме тривиального соединения аддитивных строк `Makefile`.

---

# Промпты для сессий

Ниже — то, что копируется в четыре новые сессии. Каждый промпт самодостаточен:
свежая сессия ничего не знает про этот разговор. Конвенции коммитов, правила по
Docker и планам она прочитает из `CLAUDE.md` и `.claude/rules/` в своём worktree.

## Сессия A — Playwright

```
Работай в этом каталоге (git worktree, ветка feat/web-playwright, базис —
feat/auth-gateway). Не пуш, не мержи, ветку не меняй.

Задача: шаг 10 этапа 1 — Playwright на браузерный флоу. Это последнее, что
осталось до закрытия этапа. Контекст и принятые решения — в
.claude/plans/stage-1-auth-gateway.md и .claude/plans/handoff.md, прочти их
первыми.

Свежий worktree пуст от git-ignored артефактов: начни с `make proto` и
`make web-install`, иначе ни веб, ни Go не соберутся.

Что покрыть, против поднятого через Docker стека (rules/testing.md — на голом
хосте не гонять никогда):
  1. регистрация -> профиль;
  2. вход паролем;
  3. вход через fake-провайдера целиком: кнопка -> редирект -> callback -> профиль;
  4. перезагрузка страницы не разлогинивает;
  5. document.cookie не содержит refresh-токен;
  6. выход -> профиль недоступен.

Стек: новый core/deploy/docker-compose.web-e2e.yml с `name: axon-web-e2e`,
по образцу docker-compose.e2e.yml (postgres -> migrate-auth -> auth -> gateway),
плюс сервис web и раннер Playwright. Хостовые порты не публикуй: браузер живёт
внутри сети compose. Цель Makefile назови test-web-e2e и допиши её в САМЫЙ КОНЕЦ
файла (не рядом с web-check: параллельная сессия правит `tidy` десятью строками
ниже, и вы столкнётесь). Джоб в .github/workflows/ci.yml — вместо заглушки-
комментария в конце файла.

Три вещи, которые иначе съедят у тебя час каждая:
  - При APP_ENV=test gateway ставит refresh-куку с Secure, а браузер отвергает
    Secure-куки на plaintext-origin, если это не localhost. http://web:3000 —
    не localhost. Поставь в этом стеке REFRESH_COOKIE_SECURE=false явно.
  - NEXT_PUBLIC_API_BASE_URL вшивается в бандл на сборке и должен быть адресом,
    по которому до gateway дотягивается браузер ВНУТРИ раннера, то есть
    http://gateway:8080. Для runtime-таргета это значит build-arg в
    Dockerfile.web. Гонять dev-таргет вместо runtime можно, но для Go-набора
    этот вопрос решён в пользу runtime — реши так же или обоснуй в плане.
  - CORS_ALLOWED_ORIGINS, OAUTH_REDIRECT_BASE_URL и OAUTH_ALLOWED_RETURN_ORIGINS
    должны сойтись на http://web:3000, а OAUTH_FAKE_AUTHORIZE_URL —
    http://auth:8081/oauth/fake/authorize, потому что по нему идёт браузер.

Про fake-провайдера: его authorize — это 302 без HTML-формы (см.
core/services/auth/internal/oauth/fake.go), личность зашита на сервере. Кликать
в чужом UI не нужно, достаточно дождаться возврата.

Границы. Правь только: ui/web/e2e/**, playwright.config.ts, ui/web/package.json,
core/deploy/docker-compose.web-e2e.yml, core/deploy/Dockerfile.web,
.github/workflows/ci.yml, конец Makefile. Остальной ui/web/src читай, но не
меняй; если тесту мешает разметка — заведи для этого отдельный вопрос в конце,
а не переписывай компоненты. Не трогай core/services/**, core/go.work,
docker-compose.yml, и не редактируй roadmap.md, handoff.md,
stage-1-auth-gateway.md — их обновляет интегратор.

Ты единственный, кому разрешён dev-стек: `make up` и имя проекта axon твои.
Если понадобится `make test-e2e`, запускай как
COMPOSE_PROJECT_NAME=axon-e2e-a make test-e2e.

План работы напиши в .claude/plans/stage-1-web-e2e.md до кода (rules/plans.md).
Закончи отчётом: что покрыто, что осталось, вывод прогона.
```

## Сессия B — Apple Sign In и отказ в LinkOauthAccount

```
Работай в этом каталоге (git worktree, ветка feat/auth-apple-signin, базис —
feat/auth-gateway). Не пуш, не мержи, ветку не меняй.

Две независимые задачи в модуле core/services/auth, двумя отдельными коммитами.
Контекст — .claude/plans/stage-1-auth-gateway.md (раздел OAuth) и
.claude/plans/handoff.md.

Свежий worktree не содержит генерённого кода: начни с `make proto`.

ЗАДАЧА 1. Sign in with Apple. Реальных ключей нет и не будет в этой сессии,
поэтому реестр провайдеров остаётся закрытым для Apple (он и так закрыт по
умолчанию — провайдер без полного набора креденшелов даёт
ErrProviderUnsupported, и это поведение менять нельзя). Пиши код и unit-тесты.

Apple не влезает в существующий httpProvider, и это осознанно — см. комментарий
в internal/oauth/provider.go. Три отличия:
  - client_secret это ES256-JWT, подписанный .p8: iss=team_id, sub=client_id,
    aud=https://appleid.apple.com, с ротацией по exp (не больше 6 месяцев);
  - профиль приходит в id_token внутри ответа на обмен и проверяется по Apple
    JWKS; email/email_verified/is_private_email берутся оттуда;
  - callback — form_post, и имя пользователя приходит ТОЛЬКО при первой
    авторизации отдельным полем `user`; второго шанса не будет.
Переменные OAUTH_APPLE_CLIENT_ID / TEAM_ID / KEY_ID / PRIVATE_KEY уже есть в
core/deploy/.env.example — реестр должен считать провайдера настроенным только
при всех четырёх, наполовину заполненная запись = отсутствующая.

Ловушка веб-роута: form_post не может прийти на существующую клиентскую
страницу /auth/callback/[provider] — она читает query. Нужен route handler, но
если положить его по тому же пути /auth/callback/apple, он перехватит и GET, и
редирект на самого себя зациклится. Варианты: отдельный путь
/auth/apple/callback с 303 на существующую страницу, либо особый redirect_uri
для Apple в конфиге auth. Выбери и обоснуй в плане. Из ui/web тебе разрешён
только новый каталог src/app/auth/apple/** — остальной веб правит параллельная
сессия.

Проверяемость без ключей: сгенерируй P-256 в тесте и проверь claims и подпись
client_secret, разбор id_token против подставного JWKS, отказ при
email_verified=false. E2E на Apple не заводи.

ЗАДАЧА 2. LinkOauthAccount молча сообщает об успехе, когда на самом деле
отказал. В core/services/auth/internal/repository/oauth.go:36 стоит
`ON CONFLICT (provider, provider_user_id) DO UPDATE ... WHERE
oauth_accounts.user_id = EXCLUDED.user_id`. Защита работает — identity не
переезжает к другому пользователю, — но при отказе UPDATE затрагивает ноль
строк, Exec возвращает nil, и вызывающий считает, что привязал. Последствие
видно в internal/service/oauth_flow.go:181: человека пускают в аккаунт по
адресу, привязки не возникает, и на следующем входе OauthAccountByProviderID
уводит его в ДРУГОЙ аккаунт. Проверь RowsAffected(), заведи sentinel-ошибку в
domain, реши на уровне сервиса, что происходит (отказ с внятным сообщением),
конвертируй в connect.Code* на границе, и покрой integration-тестом на две
учётки.

Границы: только core/services/auth/**, core/deploy/.env.example и новый
ui/web/src/app/auth/apple/**. Не трогай gateway, shared, go.work, Makefile,
docker-compose*.yml, CI, и не редактируй roadmap.md, handoff.md,
stage-1-auth-gateway.md.

Docker: `make up` не запускай — dev-стек занят другой сессией. Тебе доступны
`make check`, `make lint`, `make test-integration` (testcontainers, изолирован),
и e2e строго под своим именем проекта:
COMPOSE_PROJECT_NAME=axon-e2e-b make test-e2e — все 31 сценарий должны остаться
зелёными.

План напиши в .claude/plans/auth-apple-signin.md до кода. Закончи отчётом.
```

## Сессия C — движок игр и Tic-Tac-Toe

```
Работай в этом каталоге (git worktree, ветка feat/game-engine-tictactoe, базис —
feat/auth-gateway). Не пуш, не мержи, ветку не меняй.

Задача: доменная часть этапа 3 — новый модуль core/services/game, только
internal/engine (интерфейс Game + registry.go) и internal/games/tictactoe.
Контекст: .claude/plans/roadmap.md (этап 3), CLAUDE.md, .claude/rules/go-services.md.

Явно ВНЕ объёма, остаётся этапу 3 целиком: session, server, ws, proto, compose,
Dockerfile, CI-матрица образов. Причина, по которой это разделено: чистый домен
не зависит от инфраструктуры, поэтому его можно писать параллельно с чем угодно
и гонять на хосте — а всё остальное требует стека, который сейчас занят.

Интерфейс зафиксирован в CLAUDE.md и не обсуждается:

    type Game interface {
        Init(players []PlayerID) State
        ApplyMove(state State, move Move) (State, error)
        ValidMoves(state State, player PlayerID) []Move
        IsTerminal(state State) (bool, *PlayerID)
    }

Настоящая работа — выбрать представление State и Move. Ограничения: оно должно
пережить сериализацию (этап 3 хранит историю ходов в Postgres и восстанавливает
состояние реплеем), позже уехать в proto, и вместить шахматы и Quoridor, не
ломая взаимозаменяемость через registry.go. Зафиксируй решение и обоснование в
плане — это главное проектное решение задачи, а не tictactoe.

ApplyMove — чистая функция: никакого I/O и никакой мутации входного состояния.
Второе проверь тестом, а не комментарием. Тесты table-driven, без моков
(rules/go-services.md): партия до победы, партия до ничьей, отказ на занятой
клетке, ход не в свою очередь, ход вне доски, ход после терминального
состояния.

Модуль: core/services/game/go.mod (путь github.com/bvivg/axon/core/services/game),
запись в core/go.work, плюс две правки Makefile — GO_MODULES (строка 14) и
список модулей в цели `tidy`. Больше в Makefile ничего не трогай: его правят ещё
две сессии, каждая в своей секции. Если модулю не нужен shared — не тащи
зависимость ради симметрии.

Границы: core/services/game/**, core/go.work, две строки Makefile, свой файл
плана. Не трогай ничего в services/auth, services/gateway, shared, ui/web,
compose, CI, и не редактируй roadmap.md, handoff.md, stage-1-auth-gateway.md.

Docker: `make up` и `make test-e2e` не запускай — стек занят. Тебе нужно только
`cd core && go test -race ./services/game/...` (чистая доменная логика на хосте
разрешена rules/infra.md) и `make lint`. Если go-сборка не находит генерённый
код соседних модулей — сделай `make proto`.

План напиши в .claude/plans/stage-3-game-engine.md до кода. Закончи отчётом:
какое представление State/Move выбрано и почему, что покрыто тестами.
```

## Сессия D — `shared/pkg/kafka` и `shared/pkg/ws`

```
Работай в этом каталоге (git worktree, ветка feat/shared-kafka-ws, базис —
feat/auth-gateway). Не пуш, не мержи, ветку не меняй.

Задача: заготовка транспорта под этап 2 — пакеты core/shared/pkg/kafka и
core/shared/pkg/ws. Контекст: .claude/plans/roadmap.md (этап 2),
.claude/rules/infra.md, .claude/rules/observability.md.

Свежий worktree не содержит генерённого кода: начни с `make proto`.

Важная оговорка, с которой начинается план. roadmap отложил эти пакеты «до
появления реального потребителя» НАМЕРЕННО, и эта задача идёт против того
решения. Риск — спроектировать под воображаемого потребителя. Поэтому первым
делом назови в плане двух конкретных первых потребителей (fan-out chat.message
между инстансами chat и события game.started / game.finished) и проектируй ровно
под них. Всё, что не нужно этим двум, не пиши.

kafka: продюсер и консьюмер поверх контракта тем <domain>.<event>,
консьюмер-группы по сервису-потребителю, context первым параметром, graceful
close. Обязательно переиспользуй уже существующий core/shared/pkg/correlation
(Header, WithID, FromContext, Ensure): correlation_id должен уезжать в заголовки
сообщения и подниматься обратно у консьюмера — без этого трассировка рвётся на
шине, а rules/observability.md требует сквозного ID с первого дня. Выбор
библиотеки за тобой (нужны консьюмер-группы, ручной коммит и заголовки),
обоснуй в плане одной строкой.

ws: обёртка над coder/websocket — пинги, backoff при переподключении, graceful
close с кодами. Никакого знания о чате, играх и их сообщениях: это транспорт.

Тесты: unit на то, что можно проверить чисто (backoff, коды закрытия, разбор
заголовков), integration на Kafka через testcontainers за тегом integration в
core/shared/integration/ — по образцу core/services/auth/integration/. Расширь
цель test-integration в Makefile, добавив путь; больше в Makefile ничего не
трогай, его правят ещё две сессии в других секциях.

Границы: core/shared/pkg/kafka/**, core/shared/pkg/ws/**,
core/shared/integration/**, одна строка Makefile, свой файл плана. Kafka в
core/deploy/docker-compose.yml НЕ добавляй — брокер поднимает testcontainers, а
в dev-стек она приедет вместе с сервисом chat. Не трогай services/**, ui/web,
CI, и не редактируй roadmap.md, handoff.md, stage-1-auth-gateway.md.

Docker: `make up` и `make test-e2e` не запускай — стек занят другой сессией.
Тебе доступны `make check`, `make lint`, `make test-integration`.

План напиши в .claude/plans/stage-2-transport.md до кода. Закончи отчётом:
какие интерфейсы получились и под каких потребителей они спроектированы.
```
