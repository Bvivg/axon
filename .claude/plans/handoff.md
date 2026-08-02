# Handoff — состояние работ

> Живой файл: описывает, **где мы сейчас**, а не что решено — решения лежат в
> `roadmap.md` и `stage-<N>-*.md`. Обновляется в конце сессии, чтобы следующая
> (или другой человек) поднималась с нуля, не перечитывая историю.
>
> Последнее обновление: 2026-08-02.

## Где мы

Ветка `feat/auth-gateway`, **21 коммит** поверх `origin/main` (`edb7fc5`),
fast-forward — расхождения нет, force-push не нужен.

| | Статус |
|---|---|
| Этап 0 — фундамент | ✅ закрыт (PR #1, #2) |
| Этап 1 — auth + gateway + веб-логин | **9 из 10 шагов**, остался Playwright |
| Этапы 2–7 | не начинались |

Объём на сейчас: 96 Go-файлов (~16 000 строк) и 264 теста; веб — 24 файла
TS/TSX (~1 300 строк). Сгенерированный код в счёт не идёт.

Последние коммиты этапа 1:

```
291250f docs(plans): record the web client and the gateway refresh cookie
0a45fc1 ci: check the web client, and give the compose job an .env to read
db69c1b feat(web): add the Next.js client with password and provider sign-in
4803c2c feat(gateway): keep a browser's refresh token in an HttpOnly cookie
cab3c13 feat(auth): add provider sign-in with a fake, Google and GitHub
```

Проверено вручную в браузере на `http://localhost:3000`: регистрация → профиль,
F5 сессию не роняет, выход гасит куку, вход паролем, вход через fake-провайдера.
`document.cookie` пуст на всех шагах, в `localStorage` ничего.

## Что дальше, по порядку

### 1. Шаг 10 — Playwright (закрывает этап 1)

Единственное, что осталось до критерия готовности этапа. Флоу уже подтверждён
живым браузером, так что тест кодирует известно-рабочий путь, а не догадки.

Что покрывать: регистрация → профиль; вход паролем; вход через fake-провайдера
целиком (редирект → callback → профиль); перезагрузка не разлогинивает;
`document.cookie` refresh-токена не содержит; выход делает профиль недоступным.

Известный пробел: **`docker-compose.e2e.yml` сервиса `web` не содержит** — там
сейчас только Go-стек. Его нужно добавить вместе с раннером Playwright; по
`rules/testing.md` прогон идёт против поднятого через Docker стека и никогда на
голом хосте. В CI это восьмой джоб; заглушка-комментарий на этот счёт стоит в
конце `.github/workflows/ci.yml`.

### 2. PR по этапу 1

`main` защищена, мерж только через PR со squash (`rules/git-ci.md`). Ветка ещё
не запушена (`origin/feat/auth-gateway` не существует). **Пуш делает пользователь
сам** — автоматический режим его блокирует:

```bash
git push -u origin feat/auth-gateway
```

### 3. Apple Sign In

Единственный ненастроенный провайдер, вынесен намеренно: `client_secret` у него —
ES256-JWT из `.p8` с ротацией, профиль приходит только в `id_token` и только при
первой авторизации, callback — `form_post` (то есть нужен серверный роут, а не
клиентская страница, как у остальных). Сейчас ведёт себя как любой ненастроенный
провайдер: `ErrProviderUnsupported`.

### 4. Мелочь, отложенная

`LinkOauthAccount` при попытке привязать провайдера, уже занятого **другим**
пользователем, отвечает молчаливым успехом вместо явного отказа.

## Как всё запускается — только через Docker

`rules/infra.md`: на хосте не выполняется ничего, кроме `gofmt`, линтеров и
чистых unit-тестов. Всё остальное — через `make`, который ходит в compose.

| Команда | Что делает |
|---|---|
| `make env` | `.env` из `.env.example` — без него compose не стартует |
| `make proto` | генерация Go + TS; **обязательна первой** после чистого клона |
| `make up` / `make down` / `make logs` | dev-стек: postgres, redis, auth, gateway, web |
| `make check` | gofmt + vet + `go test -race` |
| `make lint` | golangci-lint, включая код за тегами `e2e`/`integration` |
| `make test-integration` | testcontainers поднимает Postgres |
| `make test-e2e` | полный стек на **runtime**-образах, сносится после прогона |
| `make web-check` | lint + typecheck + build веб-клиента через сервис `node` |

Порты на хосте: web 3000, gateway 18080 (публичный) и 19090 (админский),
postgres 55432, redis 56379. Стандартные заняты другими проектами.

## Что дорого переоткрывать заново

- **Куку ставит gateway, не веб.** Интерцептор `services/gateway/internal/cookie`.
  Браузер определяется по наличию заголовка `Origin` — подделать со стороны
  страницы нельзя, поэтому «прикинуться не-браузером и получить токен в теле» не
  сценарий. Клиенты без `Origin` (будущий connect-swift, Go-набор e2e) не задеты.
- **E2E не использует `cookiejar`** и держит куку руками: e2e-gateway работает с
  `APP_ENV=test`, то есть `Secure=true`, а jar отказался бы слать такую куку по
  plaintext-сети compose.
- **`ui/web/src/gen` в `.gitignore`.** Без `buf generate` веб не собирается, и
  падает невнятно — на отсутствующих типах. Поэтому шаг генерации есть и в
  образе, и в каждом джобе CI.
- **`NEXT_PUBLIC_*` инлайнится в бандл**, то есть читается браузером, а не
  контейнером: `NEXT_PUBLIC_API_BASE_URL` — это адрес, по которому до gateway
  дотягивается **хост**, никогда `http://gateway:8080`.
- **Реальных OAuth-ключей нет и не ожидается в репозитории.** Google/GitHub
  написаны, но в реестр не попадают без пары id/secret; весь флоу проверяется
  fake-провайдером, переход на настоящие ключи — правка `.env`, без правок кода.

## Куда смотреть

| Что | Где |
|---|---|
| Решения по этапу 1 целиком | `.claude/plans/stage-1-auth-gateway.md` |
| Общий порядок этапов | `.claude/plans/roadmap.md` |
| Конвенции (Go, proto, web, infra, тесты, git, security) | `.claude/rules/*.md` |
| Refresh-кука | `core/services/gateway/internal/cookie/` |
| Веб-клиент, auth-слой | `ui/web/src/lib/auth/`, `ui/web/src/lib/connect/` |
| E2E-набор | `core/services/auth/e2e/` |
