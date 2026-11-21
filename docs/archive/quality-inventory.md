# Инвентаризация качества Axon — шаг 1

18 ноября 2026 · ветка `fix/axon-quality-audit`.

Это список предложений для согласования. Исправления шагов 2–5, массовые перемещения и удаления не выполнялись. Новые артефакты этого этапа — этот отчёт и `docs/design/before/`. Существующий локальный `PROGRESS.md` обновлён для продолжения работы.

Аудит исходников сделан по текущему рабочему дереву, включая 390 исходных dirty/untracked путей. В них нельзя записывать весь будущий коммит: это уже существовавшая работа. Исходный staged diff сохранён отдельно. Сборка могла переписать generated `next-env.d.ts`; возвращено его исходное содержимое, полный tracked worktree diff снова совпадает с исходным побайтно.

UI проверен на работающем Docker-стенде [https://axon.test](https://axon.test). Снимки показывают именно существующую сборку, не новый деплой текущего dirty tree: web `axon-web:tictactoe-result-alignment`, gateway/game `*:network-game`, auth/chat `*:project-review`. Точные image IDs записаны в `docs/design/before/stand.json`. Не утверждается, что каждый исходный незакоммиченный файл включён в эти образы.

При проверке зависимостей обнаружено заполнение диска Docker. После отдельного разрешения владельца очищены неиспользуемые образы/контейнеры/сети/build-cache (`docker system prune --all --force`), освобождено 33,18 ГБ. Volumes не удалялись, PostgreSQL восстановился с исходными данными. Это обслуживание стенда, не чистка списка ниже.

## Решения перед следующими этапами

1. Согласовать пофайловый список А и предлагаемые правки Б/В/Г. До этого ничего из списка не удалять и не переносить.
2. Не создавать отдельный calling-процесс ради старой схемы. Существующий Calls — модуль Chat с контрактом `axon.calls.v1`; изменение процесса потребует отдельного решения.
3. Не чинить несуществующий missing-game дефект: `game-migrate` и `game` уже есть в текущем web-e2e Compose, проверка `config --quiet` проходит.
4. Добавить patch-update Next.js в начало согласованного этапа качества: текущая сборка остановилась на security check. Это предложение, upgrade ещё не запускался.
5. По отдельному разрешению владельца создан новый тестовый admin. Сняты Admin/Service map/Gateway в обеих темах на трёх ширинах; существующие роли других пользователей не менялись. Аппаратные звонки остаются отдельной ручной проверкой.

## А. Служебные артефакты


| Путь | Состояние | Объём | Предлагаемое действие | Причина / условие |
|---|---|---:|---|---|
| `.claude/plans/` | 24 tracked, 5 untracked | 29 файлов, 499,5 КиБ | **docs/archive**, кроме временно оставленного `calls-hybrid.md` | Здесь есть полезные решения, но рядом лежат устаревшие инструкции следующей сессии. Архивировать как историю с датой, не выдавать за текущую документацию. Пофайловый список ниже. |
| `.claude/rules/` | 12 tracked | 37,2 КиБ | **оставить**, с исключениями ниже | Правила API, безопасности, транзакций и тестов — инженерные требования. Их удаление ради отсутствия агентных имён ухудшит качество. Можно позже перенести действующие нормы в CONTRIBUTING/docs/development отдельным согласованным изменением. |
| `.mcp.json` | tracked, изменён | 447 Б | **убрать из репозитория + .gitignore** | Настройка локального клиента с локальным DSN, `npx ...@latest` и зависимостью от доступных инструментов. Реальных внешних секретов не утверждаю: в файле дев-доступ. Если нужен командный пример — оставить отдельно обезличенный `.mcp.example.json`, без подключений к конкретной машине. |
| `AGENTS.md` | untracked, не ignored | 10 193 Б | **оставить** | Один короткий источник обязательных правил проекта полезен. Сократить до инженерных инвариантов и команд, убрать дублирующее дерево будущей архитектуры. Сейчас побайтно совпадает с `CLAUDE.md`. |
| `CLAUDE.md` | tracked, изменён | 10 193 Б | **убрать из репозитория + .gitignore** | Полный дубль AGENTS.md. Удалять из Git только после сохранения действующих правил в одном утверждённом AGENTS.md; локальный адаптер при необходимости генерируется/копируется пользователем. |
| `ui/web/AGENTS.md` | ignored через `.git/info/exclude:22` | 679 Б | **убрать из репозитория + .gitignore** | В истории его уже нет; зафиксировать общий ignore. Блок прямо говорит, что `next dev` его воссоздаёт. Исключение из Git не означает физическое удаление и не отключает чтение документации Next.js. |
| `PROGRESS.md` | ignored через `.git/info/exclude:18` | 219 477 Б | **убрать из репозитория + .gitignore** | Уже локальный журнал. Оставить физически для продолжения работ; внести общий ignore. Не переносить сырой журнал в публичный архив: там машинные адреса, ссылки на локальные профили/доказательства и служебные значения. Значимые решения извлечь отдельно без этих данных. |
| `docs/reviews/` | все 28 файлов untracked | 223 056 Б | **docs/archive** | Это датированная история ревью с очищенными доказательствами, не runtime-документация. Архивировать единым набором с относительными ссылками, сохраняя доказательства рядом с выводами. Не удалять доказательства, а затем оставлять неподкреплённое «проверено». |
| `ui/web/test-results/` | все 5 файлов untracked | 43 165 Б | **убрать из репозитория + .gitignore** | Перезаписываемые артефакты Playwright: `.last-run.json`, два `error-context.md`, два `trace.zip`. Оставлять локально/CI artifacts с retention, не в исходниках. |

Все массовые перемещения и удаления требуют согласования этого списка. Ниже перечислен точный объём. Новые `docs/design/before` из текущей задачи не относятся к автоматически удаляемому мусору: пользователь специально запросил сравнительный набор.

### Пофайловый список планов

| Файл в `.claude/plans/` | Git | Размер, КиБ | Решение | Причина |
|---|---|---:|---|---|
| `auth-apple-signin.md` | tracked | 16.1 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `auth-profile-and-sessions.md` | tracked | 15.5 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `auth-rbac.md` | tracked | 8.6 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `axon-ux-audit.md` | untracked | 14.6 | **docs/archive** | Аудит с датой 13 ноября, не текущая оценка после последующих исправлений. |
| `calls-hybrid.md` | untracked | 105.5 | **оставить** | До переноса ADR-1 и границы Store/Signaling/MediaProvider в публичные docs: на него сейчас ссылаются правила и реализация звонков. |
| `calls-ux.md` | untracked | 37.8 | **docs/archive** | История волн и утверждённых макетов; оставшиеся требования сначала вынести в отдельный backlog. |
| `chat-albums.md` | tracked | 5.2 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-groups-and-message-actions.md` | tracked | 15.2 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-live-status.md` | tracked | 10.9 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-media-messages.md` | tracked | 12.4 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-media-pipeline.md` | tracked | 10.0 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-message-compression.md` | untracked | 2.7 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-notifications.md` | tracked | 7.3 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-port-from-nod.md` | tracked | 15.8 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-rich-messaging.md` | tracked | 20.5 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-search.md` | tracked | 6.9 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-selection.md` | tracked | 6.9 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-transcript.md` | tracked | 5.6 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `chat-upload-preflight.md` | untracked | 1.4 | **docs/archive** | План реализации; актуальная эксплуатационная документация уже в docs/chat-upload-preflight.md. |
| `chat-username-urls.md` | tracked | 10.6 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `handoff.md` | tracked | 9.8 | **docs/archive** | Исторический handoff от 10 августа: утверждает, что Chat и Game server отсутствуют; не годится как текущий статус. |
| `parallel-sessions.md` | tracked | 35.4 | **убрать из репозитория + .gitignore** | Операционные указания для параллельных агентных сессий, не контракт продукта. |
| `roadmap.md` | tracked | 23.2 | **docs/archive** | Смешивает greenfield-контекст с поздними правками; исходный план сохранить исторически, текущий статус перенести в README. |
| `stage-1-auth-gateway.md` | tracked | 28.9 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `stage-1-web-e2e.md` | tracked | 14.1 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `stage-2-chat.md` | tracked | 16.5 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `stage-2-transport.md` | tracked | 17.6 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `stage-3-game-engine.md` | tracked | 15.0 | **docs/archive** | План/история отдельной фичи; перед переносом извлечь действующие контракты и причины решений, остаток пометить историческим. |
| `web-design-signal.md` | tracked | 9.6 | **docs/archive** | Предыдущая дизайн-концепция и инструкции реализации; текущие токены и UI уже отличаются. |

### Пофайловый список правил

| Файл в `.claude/rules/` | Решение | Причина |
|---|---|---|
| `api-contracts.md` | **оставить** | Действующие инженерные конвенции, не служебный отчёт. Публичные нормы можно позже перенести в CONTRIBUTING/docs/development. |
| `comments.md` | **оставить** | Оставить файл, но переписать запрет :3–19: текущий запрос разрешает объяснение неочевидного «почему», а правило сейчас запрещает даже его ради экономии токенов. |
| `git-ci.md` | **оставить** | Действующие инженерные конвенции, не служебный отчёт. Публичные нормы можно позже перенести в CONTRIBUTING/docs/development. |
| `go-services.md` | **оставить** | Действующие инженерные конвенции, не служебный отчёт. Публичные нормы можно позже перенести в CONTRIBUTING/docs/development. |
| `infra.md` | **оставить** | Действующие инженерные конвенции, не служебный отчёт. Публичные нормы можно позже перенести в CONTRIBUTING/docs/development. |
| `mcp-tools.md` | **убрать из репозитория + .gitignore** | Локальная инструкция по инструментам, уже расходится с .mcp.json: обещает docker/github, которых в конфиге нет. |
| `nextjs-web.md` | **оставить** | Действующие инженерные конвенции, не служебный отчёт. Публичные нормы можно позже перенести в CONTRIBUTING/docs/development. |
| `observability.md` | **оставить** | Действующие инженерные конвенции, не служебный отчёт. Публичные нормы можно позже перенести в CONTRIBUTING/docs/development. |
| `plans.md` | **убрать из репозитория + .gitignore** | Регламент хранения агентных планов; после выделения ADR заменяется коротким правилом ведения архитектурных решений. |
| `security.md` | **оставить** | Действующие инженерные конвенции, не служебный отчёт. Публичные нормы можно позже перенести в CONTRIBUTING/docs/development. |
| `swiftui-app.md` | **docs/archive** | Проектирование будущего клиента: ui/app отсутствует; сохранить как план, не как правила существующего кода. |
| `testing.md` | **оставить** | Действующие инженерные конвенции, не служебный отчёт. Публичные нормы можно позже перенести в CONTRIBUTING/docs/development. |

### Точный состав исторического ревью

Все перечисленные ниже пути — untracked; рекомендация **docs/archive**. Целевая основа: `docs/archive/reviews/2026-11-14-15/`, с сохранением взаимного расположения или исправлением относительных ссылок. Ничего не переносилось.

- `docs/reviews/2026-11-14/appendices.md` (64,360 Б)
- `docs/reviews/2026-11-14/commit-plan.md` (8,150 Б)
- `docs/reviews/2026-11-14/evidence/README.md` (1,141 Б)
- `docs/reviews/2026-11-14/evidence/avatar-check.json` (89 Б)
- `docs/reviews/2026-11-14/evidence/backup-restore-local.log` (631 Б)
- `docs/reviews/2026-11-14/evidence/browser-acceptance.log` (366 Б)
- `docs/reviews/2026-11-14/evidence/browser-regression-fixed.log` (1,595 Б)
- `docs/reviews/2026-11-14/evidence/contrast.json` (693 Б)
- `docs/reviews/2026-11-14/evidence/core-e2e-auth-final.log` (6,642 Б)
- `docs/reviews/2026-11-14/evidence/core-e2e-chat-final.log` (2,858 Б)
- `docs/reviews/2026-11-14/evidence/dependency-fixes.log` (350 Б)
- `docs/reviews/2026-11-14/evidence/go-acceptance.log` (5,094 Б)
- `docs/reviews/2026-11-14/evidence/go-final-security.log` (469 Б)
- `docs/reviews/2026-11-14/evidence/go-lint-final.log` (50 Б)
- `docs/reviews/2026-11-14/evidence/govulncheck-before.log` (261 Б)
- `docs/reviews/2026-11-14/evidence/media-cookie-follow-up.log` (1,285 Б)
- `docs/reviews/2026-11-14/evidence/private-media-integration.log` (117 Б)
- `docs/reviews/2026-11-14/evidence/runtime-acceptance.json` (1,095 Б)
- `docs/reviews/2026-11-14/evidence/security-before.json` (275 Б)
- `docs/reviews/2026-11-14/evidence/stand-final.txt` (415 Б)
- `docs/reviews/2026-11-14/evidence/ui-check.json` (6,135 Б)
- `docs/reviews/2026-11-14/evidence/web-final.log` (274 Б)
- `docs/reviews/2026-11-14-project-review.md` (2,814 Б)
- `docs/reviews/2026-11-15-fix-results.md` (8,445 Б)
- `docs/reviews/assets/2026-11-14/desktop-dark.png` (33,875 Б)
- `docs/reviews/assets/2026-11-14/desktop-light.png` (37,817 Б)
- `docs/reviews/assets/2026-11-14/mobile-dark.png` (16,251 Б)
- `docs/reviews/assets/2026-11-14/mobile-light.png` (21,509 Б)

### Точный состав локальных результатов Playwright

Для всех рекомендация **убрать из репозитория + .gitignore**, физическое удаление не требуется:

- `ui/web/test-results/.last-run.json`
- `ui/web/test-results/tic-tac-toe-practice-modes-6bd13-board-and-responsive-themes-chromium/error-context.md`
- `ui/web/test-results/tic-tac-toe-practice-modes-6bd13-board-and-responsive-themes-chromium/trace.zip`
- `ui/web/test-results/tic-tac-toe-two-players-in-12ac3--a-third-player-cannot-join-chromium/error-context.md`
- `ui/web/test-results/tic-tac-toe-two-players-in-12ac3--a-third-player-cannot-join-chromium/trace.zip`


## Б. Код и качество

### Независимая проверка исходных утверждений

- Chat:14843 production Go строк (74 файла), Auth:5640 (49), Gateway:2410 (20), Game:1396 (13), Shared:3117 (33). Без tests/generated/integration/e2e; это физические строки с пробелами и комментариями. Доля Chat54,16% с shared,61,11% среди четырёх сервисов; по непустым56,04%/62,70%. Исходные55% близки к первой метрике.
- Тяжёлые Chat файлы: `internal/server/handler.go`672 строки, `server/calls.go`231, `ws/handler.go`791; дополнительно `service/service.go`826, `uploads/manager.go`568. CallsHandler уже отдельный тип.
- Все шесть названных «неразобранных графом» файлов прочитаны целиком: `auth/internal/service/{refresh,session,rbac,avatar}.go` и `chat/internal/calls/{groups_service,invitations}.go`. Они компилируются; ошибка графового извлечения не является дефектом исходников. Находки и нулевые зоны тестов перечислены ниже.
- В web-e2e Compose есть `game-migrate:187`, `game:196`, URL `game:8083` и depends_on; `config --quiet` PASS. Полный e2e-стек этим не объявляется проверенным.



### B1 / высокий риск: при reuse refresh-токена нет события немедленного отзыва открытого WS

- `core/services/auth/internal/service/refresh.go:87` — `revokeCompromisedFamily` вызывает `RevokeFamily` и пишет лог, но не `publishFamilyRevocation`.
- Для сравнения: обычный Logout вызывает публикацию в `refresh.go:136`, RevokeSession — в `session.go:71`, публикация реализована в `session.go:76`.
- `core/services/chat/internal/ws/handler.go:236` закрывает WS по expiry; `:251` подписывается на Redis-событие отзыва. Обработка обычных сообщений `:281` не проверяет активность семейства в БД на каждом кадре. В repository.RevokeFamily только UPDATE; скрытой публикации нет (`auth/internal/repository/refresh_token.go:54`).
- Следствие по коду: успешный отзыв семейства в БД из-за reuse не обязан немедленно закрыть уже открытый чатовый WS. Calls дополнительно проверяет семейство при lease/preflight; это не заменяет отзыв обычного чата.
- После одобрения: сначала регрессионный тест с recording publisher, затем публикация после успешного RevokeFamily; отдельно проверить действующий WS на сценарии reuse. Сейчас дефект подтверждён статической цепочкой, end-to-end воспроизведение в этой инвентаризации не выполнялось.

### B2 / средний риск: тестовое покрытие транспортных и внешних границ значительно хуже доменных функций

Измерения приведены ниже. Конкретные нулевые области:

- `core/services/gateway/internal/proxy/auth.go:156` и прочие proxy — общий allowlist заголовков, forwarding ctx/IP/auth, ошибки upstream, пустые/неподходящие upstream: в default unit-профиле package proxy 0%.
- `gateway/internal/{attachmentproxy,avatarproxy,mediaproxy,livekitproxy}` — 0% в этом профиле. Здесь стоит закреплять авторизацию объекта, Range/304/redirect/SSRF и разрешённые маршруты HTTP-тестами с реальным httptest upstream.
- `chat/internal/server/handler.go:103` и остальные Chat RPC handlers — package server 0,71%. Имеющийся calls_test.go проверяет главным образом преобразование кодов ошибок, не полный handler boundary.
- `chat/internal/calls/groups_service.go:12` (`FromCall`) и `:32` (`SfuCredentials`) — 0%; `invitations.go:9` (`InvitationDeadline`) — 0%. Для deadline нужны direct/group, invited/accepted, exact expiry, waiting_cleanup; для SFU — invalid ref, unavailable provider, selection/generation и неудачная выдача credentials. Это отсутствие проверенного покрытия, а не доказательство ошибки ветвления.
- `auth/internal/service/avatar.go:14` — 23,53%; успешная загрузка/сохранение, импорт provider avatar, custom-avatar precedence и ошибки store/provider требуют отдельных сценариев.
- `shared/pkg/revocation` — 0%; важно проверить каналы family/user и отмену подписки, затем связанный B1.

После одобрения: писать тесты на реальные наблюдаемые контракты, не повышать процент тестами на очевидные forwarding-функции без проверок security headers/ошибок. Существующие integration/e2e тоже учитывать, а не объявлять код вообще непроверенным по unit-профилю.

### B3 / архитектурный риск: большой Chat, но Calls уже имеет разумную доменную границу

- `chat/internal/calls/store.go:10` — Store; `calls/service.go:23` — Families/Peers/Publisher; `calls/livekit.go:14` — MediaProvider; отдельный `server/CallsHandler` в `server/calls.go:19`.
- Связка общего WS и конкретного Calls Service остаётся в `ws/handler.go:60`, `:182`, `:268`, `:281`.
- `repository/calls_groups.go:86` проверяет приглашения через `rooms`/`room_members`; `repository/calls.go:171` блокирует/читает комнаты в транзакции. Значит, выделение процесса не сводится к перемещению папки: есть транзакционная связь с Chat и общая схема.
- `.claude/plans/calls-hybrid.md:22` (ADR-1) прямо запрещает создавать отдельный calling-сервис в согласованной архитектуре. Несовпадение старой картинки с реализацией нужно исправлять в документации, а не принимать за требование на новый процесс.
- После одобрения: оставить Calls модулем Chat, разделять организационные обязанности handler/service/repository/ws внутри процесса; общий WS может зависеть от узкого интерфейса сигналинга. До переноса процесса отдельно согласовать владение rooms/member checks, транзакциями и Store adapters. Нельзя обещать «разнести без риска» только на основании размера файлов.

### B4 / низкий риск: 64 однотипных RPC forwarding-метода, но общий хелпер уже есть

- `gateway/internal/proxy/auth.go:156` — существующий `forward[T]` с allowlist auth/User-Agent и доверенным ClientIP из ctx.
- Тонкие методы: auth 18, chat 19, calls 13, calls_groups 7, calls_migration 3, game 4. Каждый лишь делегирует `client.Method(ctx, forward(ctx, req))`.
- Это механическое повторение, однако Go generic helper не может автоматически реализовать интерфейс с разными именованными RPC-методами. Ещё один общий helper здесь почти ничего не сократит. Рефлексия ухудшит статическую проверку контракта.
- После одобрения: сначала тесты forwarding-политики, затем решить, окупается ли небольшой генератор по proto/Connect descriptors; иначе оставить прозрачные typed wrappers и вынести общий helper из auth.go в отдельный transport-файл. Не переписывать transport ради внешнего сходства строк.

### B5 / косметика: ненужная проверка совместимости стандартного HTTP-клиента

`gateway/internal/proxy/calls.go:27`: `var _ connect.HTTPClient = (*http.Client)(nil)`. Импорт net/http в этом файле нужен только для этой строки; она проверяет известную совместимость stdlib, а не собственную реализацию. Возможный кандидат на удаление после одобрения. Соседняя проверка `CallsServiceHandler = (*Calls)(nil)` полезна: ловит рассинхронизацию с generated RPC, её оставить.

### Что не следует чистить автоматически

- В проверенном production Go (без generated) нет полных строк `//` с пересказом очевидного. Массовая чистка комментариев не обоснована. Inline `//nolint:gosec` у используемого TURN HMAC SHA-1 — техническое исключение, не литературный шум.
- Комментарии к sibling-module replace в go.mod объясняют **почему** нужен replace при standalone Docker build, соответствуют критерию пользователя, их не удалять только из-за одинакового текста.
- Общий интерфейс Game/Registry реально используется Tic-Tac-Toe и является явной целью проекта. Один реализованный game не делает его автоматически лишней абстракцией.
- Реализован только `game/internal/games/tictactoe`; боевых Go-заготовок chess/checkers/durak/quoridor в файлах нет. Отсутствие других игр — вопрос честности roadmap, а не разрешение массово удалять дерево.
- Прочитанные refresh/session/rbac-тесты не выглядят «мок вернул то, что сказали»: проверяются rotation/reuse-family isolation, права чужой сессии, реальное изменение ролей; presence использует miniredis. `TestConcurrentExchangesAreTreatedAsReuse` моделирует competing exchange через callback, поэтому сам по себе не доказывает DB race. Для настоящей гонки существует integration `auth/integration/refresh_test.go:16`, в этом прогоне она не запускалась.


### B6 / средний риск: ChatRoomView совмещает независимые обязанности

`ui/web/src/components/chat/chat-room-view.tsx:87` — 839 строк, 43 import declarations, из них 36 локальных модулей; 17 useState, 14 useEffect, 11 useMemo, 9 useRef, 6 useCallback. Это измерения TypeScript AST, а не «100 связей» из графа. Нельзя приравнивать число рёбер графа к числу runtime-зависимостей.

Внутри находятся read/unread bookkeeping (`:336–386`), поиск и подгрузка истории для перехода к сообщению (`:398–441`), selection/forward/delete и диалоги (`:698–836`), drag/drop uploads, данные профилей, WS и rendering. `transcript.tsx` — ещё 1020 строк, `room-media.tsx` — 239, `chat-activity.tsx` — 108. Размер — признак ответственности для разбора, не самостоятельный функциональный дефект.

После согласования: сначала закрепить сценарии unread/read, возврата из поиска, selection/forward/delete, draft persistence и reconnect. Затем разделить управление чтением, переходом по истории и message actions; выделять компоненты только с понятной входной моделью, не новый универсальный controller/framework.

### B7 / средний риск: unit-тестов компонентов чата нет

Есть 19 файлов `.test.ts` и 92 проходящих теста, преимущественно pure logic. Тестов `.test.tsx`/компонентов нет; `ui/web/vitest.config.ts:10–11` включает только `src/**/*.test.ts` и использует `environment: "node"`. В `package.json` отсутствуют Testing Library и DOM test environment. Фраза «3% символов» не является измерением statement/branch coverage и здесь не подтверждается как такой процент.

После согласования: добавить проверенные совместимые Testing Library/DOM test dependencies и тесты пользовательского поведения chat components. Не заменять существующие Playwright e2e: draft, menu keyboard и WS integration всё ещё требуют проверки через браузер. Не переписывать хорошие pure-function тесты только ради новой библиотеки.

### B8 / низкий риск: неиспользуемая альтернативная Sidebar-система

`ui/web/src/components/ui/sidebar.tsx:25–375` содержит собственный context, keyboard shortcut, mobile/desktop layout и набор экспортов. Поиск imports/reexports и `SidebarProvider`/`useSidebar` вне этого файла не нашёл потребителей; действующая оболочка использует `components/nav/app-sidebar.tsx` и `context-sidebar.tsx`.

Кандидат на удаление после утверждения — **только `ui/web/src/components/ui/sidebar.tsx`**. Связанные общие Button/ScrollArea/Tooltip/use-mobile не удалять по транзитивной догадке: у них могут быть другие потребители. Это локальный остаток готового набора, а не повод удалять всю папку `components/ui`.

### B9 / продуктовая заглушка: демонстрационные игровые профили

`ui/web/src/lib/games/tictactoe-profiles.ts:1–5` задаёт фиксированные имена и рейтинги, `components/games/tictactoe/practice.tsx:66–68` показывает их в local-режиме. Сценарий подтверждён в браузере и обеих темах. Предлагается нейтральная маркировка локальных участников/демонстрации; не реализация рейтингов ради удаления TODO. См. VIS-12.

### B10 / блокирует сборку: известные advisory у установленного Next.js

Установлен `next@16.4.4`. `npm run build` сгенерировал production output, но завершился **exit 1 / SecurityFatalError**, поэтому сборка не отмечена успешной. `npm audit --omit=dev --json` независимо подтвердил один уязвимый package с двумя moderate advisory:

- [GHSA-mfr8-8qww-v76h — первичное уведомление Next.js](https://github.com/vercel/next.js/security/advisories/GHSA-mfr8-8qww-v76h).
- [GHSA-vc7v-qv4w-wrv8 — первичное уведомление Next.js](https://github.com/vercel/next.js/security/advisories/GHSA-vc7v-qv4w-wrv8).

Оба исправлены в 16.4.5. Уведомления требуют определённых Cache Components/root-param сценариев. В `ui/web/src` и `next.config.ts` не обнаружены `use cache`, `unstable_cache`, `next/root-params` или включённый `cacheComponents`, поэтому эксплуатируемая утечка данных именно в Axon **не доказана**. Обновление patch-version с повторной сборкой/тестами предлагается отдельно после одобрения; `npm audit fix` и upgrade-команды не запускались.

### B11 / воспроизводимый дефект: Skip to content ломает адрес чата

На `/chat` фокусируем Skip to content и нажимаем Enter. URL становится `/chat#app-content`, фокус остаётся на ссылке, чат разрешает этот hash как адрес беседы и возвращает Chat not found. На мобильной ширине основная область может оказаться пустой, потому что missing-placeholder скрыт layout-классами.

Цепочка: `ui/web/src/components/nav/app-shell.tsx:65` — обычная ссылка `href="#app-content"`; `lib/chat/address.ts:212` читает hash как chat address; `parseChatAddress:36–46` отвергает `app-content`; `components/chat/chat-address-view.tsx:101–102` показывает missing. Воспроизведено клавиатурой на390/768/1440 в обеих темах, кадры `chat-skip-error.png`.

После согласования: регрессионный тест должен проверять фокус `#app-content` и сохранение текущего chat hash при skip-навигации; затем развести focus navigation и chat addressing. Адресные ссылки и browser back должны сохраниться. На этом этапе код не менялся.

## В. Дизайн


| ID | Приоритет | Что видно | Доказательство / код | Что предложить в утверждаемый список |
|---|---|---|---|---|
| VIS-01 | P2 | В Appearance карточки Light и Dark не показывают разные варианты. В светлой теме обе светлые, в тёмной обе тёмные; две половины System тоже выглядят одинаково. Подписи и radio работают как обозначения выбора, но сами previews не объясняют разницу. | `light/390/appearance.png`, `light/1440/appearance.png`, `dark/390/appearance.png`. `ui/web/src/components/profile/appearance.tsx:44–45,66–72`: scheme меняется через inline `colorScheme`, но цветовые классы используют общие inherited theme tokens. | После согласования привязать preview-палитры к собственным light/dark значениям, а не только к текущему режиму корня. Основной DOM-аудит подтвердил: при dark у всех четырёх preview-фрагментов фон rgb(14,18,27), включая scheme=light; в light все светлые. Это функциональный дефект визуального переключателя, не вопрос предпочтений. |
| VIS-02 | P2/P3 | Username в форме регистрации заметно ниже соседних полей на 768/1440: около32px против43–44px у Email/Display name/Password. На390 высоты выравниваются. Меняется ритм формы и радиус заметно острее. | `light/1440/register.png`, `dark/1440/register.png`, `light/768/register.png` против `light/390/register.png`. `credentials-form.tsx:138` использует UsernameInput без auth-field; Email/Display/Password получают его. `auth/username-input.tsx:11` не принимает отдельный wrapper class. `workspace.css:249` настраивает только `.auth-field`. | Свести размеры и радиусы связанных полей через один size/token. Закрепить компонентной проверкой формы, затем PNG/DOM в трёх размерах. Не менять валидацию username ради косметики. |
| VIS-03 | P2 (UX/feature gap) | Из формы входа есть Forgot password?, но переход ведёт к странице, на которой доступно только «Password reset is currently unavailable» и Back to sign in. Это честный текст, но для пользователя, потерявшего пароль, путь заканчивается без решения. | `light/390/login.png` → `light/390/forgot-password.png`, также `light/1440/forgot-password.png`. `credentials-form.tsx:196` показывает ссылку. | Отдельно согласовать полноценное восстановление или ясный доступный пользователю альтернативный путь; не маскировать отсутствие функции красивой формой и не выполнять эту новую auth-фичу попутно в визуальной чистке. |
| VIS-04 | P3 | Auth desktop состоит из типового рекламного разворота: «Your space to connect.», полупрозрачная наклонённая карточка «Design team», воображаемый разговор «The new design is ready / Great, let’s take a look / See you in the call». Это не объясняет особенностей Axon и выглядит шаблонной демонстрацией рабочего мессенджера. Логотип/название дважды — в navbar и story. | `light/1440/login.png`, `dark/1440/register.png`, `light/768/register.png`. `auth/auth-story.tsx:6–17`, radial gradients и rotate(-2deg) в `workspace.css:242,250`. | Пометить story/copy к переработке на шаге4 после выбора направления. Не приписывать этому происхождение «ИИ»: это оценка специфичности и полезности композиции. Не выбирать новую палитру/бренд сейчас. |
| VIS-05 | P3 | Пустой Chat говорит «No chats yet», а правая часть «Select a chat to start messaging», хотя выбирать нечего. Следующий шаг спрятан в одной иконке compose, а центральная пустота ничем не помогает начать. | `light/1440/chat.png`, `dark/1440/chat.png`, `light/390/chat.png`, `light/768/chat.png`. | Отдельное осмысленное first-use empty state с понятным действием. Это отличается от настоящего «чат не выбран» при уже существующих диалогах; эти два состояния не должны сообщать одно и то же. |
| VIS-06 | P3 | Пустые состояния устроены неодинаково: Media — большая карточка с иконкой/текстом/Open chats; Chat — только строка в пустом поле; Calls — иконка и «No calls yet», повторённое ещё и в колонке. В обоих Calls и Media главная мысль отделена от действия пустым пространством/другой частью layout. | `light/1440/{chat,calls,media}.png`, `dark/1440/{chat,calls,media}.png`, `light/390/{chat,calls,media}.png`. | Согласовать общие правила empty/loading/error и у каждого состояния свой полезный следующий шаг. Сами сообщения тематические, не один бессмысленный универсальный «Something went wrong»; это нужно сохранить. |
| VIS-07 | P3 | В Contacts на390 поле Search people занимает только около222px при доступной строке366px, тогда как chat/calls search растянуты почти на весь экран. На1440 поле закономерно находится справа; мобильный перенос оставляет случайную пустую зону. | `light/390/contacts.png`, `dark/390/contacts.png`, сравнение с `light/390/chat.png` и `light/390/calls.png`. `app/(app)/contacts/page.tsx:31–34`: w-full есть на input, но wrapper внутри flex wrap не растянут. | Растянуть контейнер поиска на мобильной ширине, сохранив ограничение справа на desktop. Проверить длинный запрос и экранную клавиатуру. |
| VIS-08 | P3 | На1440 одна и та же трёхколоночная оболочка применяется к разным задачам. Для чата320px контекстный список оправдан, но Contacts получает320px под единственную All contacts; Media — под две ссылки. В итоге заголовки повторены в соседних колонках, полезная ширина формы/каталога сокращена. На Calls одновременно видны Calls в nav, контекстном заголовке, обратной ссылке и главном заголовке. | `light/1440/{contacts,media,calls}.png` и тёмные аналоги. `workspace.css:7,17,74–80` фиксирует context column320px; `calls/call-history.tsx:58–60` добавляет обратную Calls-ссылку перед одноимённым h1. | При выборе общей системы решить, где контекстная колонка несёт настоящую навигацию/данные, а где достаточно локального заголовка. Это архитектура информации, не повод массово переписать shell без согласования. |
| VIS-09 | P3 | Profile desktop растягивает Email, Username и Nickname почти на всю доступную ширину; на390 readonly Email с длинным значением конкурирует с бейджем Unverified и видимое значение обрезано. Небольшая Save changes ~32px выглядит второстепенной по сравнению с большими полями44px и кнопками auth. | `light/1440/profile.png`, `dark/1440/profile.png`, `light/390/profile.png`. `profile/profile-page.tsx:23` допускает max-w-4xl для всего; `workspace.css:97` уменьшает min-h-11 до36px в workspace. | Развести ширину полей по смыслу, определить один ряд размеров controls, длинные readonly значения и badge не должны мешать чтению. Touch-target надо измерить DOM: размер32px сам по себе не доказывает нарушение WCAG AA. |
| VIS-10 | P3 | В Sound settings отмеченные checkbox выглядят нативно-синими, тогда как Appearance radio и основные действия фиолетовые. Пользователь видит другую систему controls на соседней вкладке настроек. Это расхождение оформления, не причина заменять доступный native input. | `light/390/notifications.png`, `profile/notifications.tsx:66–67`: обычный input без accent-color/своего класса. Финальные снимки пересняты после загрузки: Browser notifications честно показывает Not set up on this server. | После выбора токенов применить один accent к нативным controls или переиспользовать согласованный компонент, сохранив label/keyboard semantics. |
| VIS-11 | P3 | Games desktop снова оформлен как рекламная посадочная: «One place. More connection.», «Pick a game. Bring your people.», «A little friendly rivalry.», большой градиентный баннер с декорацией. При одной доступной игре вход дублируется в sidebar, CTA и строке каталога. На390 маркетинговый блок скрыт, остаётся короткий каталог — это более прямой пользовательский путь. | `light/1440/games.png`, `light/390/games.png`; `games/catalogue.tsx:49–56`. | Вынести hero/copy в список дизайна шага4. Сначала решить, что пользователь должен сделать при одной игре; не удалять декоративный блок без утверждения списка. |
| VIS-12 | P2 | В локальной Practice показаны имена nomad_01 и ak_mergen с рейтингами1488/228 без видимого обозначения демонстрационных данных. Это производит впечатление настоящих профилей/рейтинга, хотя числа константные. | `light/390/practice.png`, `light/1440/practice.png`; `lib/games/tictactoe-profiles.ts:1–4` прямо содержит TODO заменить mock profiles; `practice.tsx:66–68` выводит их в local-режиме. | После согласования дать локальным участникам честные нейтральные обозначения/настройку имени или явно отделить демонстрацию. Не создавать backend rating ради удаления TODO. Это конкретная продуктовая заглушка, не только комментарий. |
| VIS-13 | P3 | Игровой экран озаглавлен «Крестики-нолики», игрок-бот — «Компьютер», в то время как shell и каталог предлагают Games / Tic-Tac-Toe / Quick match и остальная форма настроек англоязычная. Смена языка не объяснена. | `light/390/practice.png`, `light/1440/practice.png` против games.png; `tictactoe-profiles.ts:5`, строки Arena. | Зафиксировать языковую политику продукта до переписывания copy. Смешение не лечится сменой шрифта; существующие строки могли быть сознательным выбором владельца, поэтому это вопрос согласования, а не автоматическая замена. |



### Админка: дополнение после предоставления доступа

- `admin/service-map.tsx:44–46` показывает Services=9, но `lib/operations/topology.ts:6–15` считает Web, Gateway, Auth, Chat, Game и четыре инфраструктурных узла. Предлагается точная подпись Components/Nodes либо отдельный подсчёт сервисов; это семантика числа, не ошибка доступности.
- Карта Code topology — вручную поддерживаемый ограниченный срез: Calls обозначен внутри Chat, Kafka producer помечен optional. LiveKit/coturn/media и часть storage-edges не показаны. Не выдавать её за автоматически обнаруженный полный runtime graph. Runtime unavailable уже честно подписан.
- Мобильная схема заменена каталогом компонентов, не обрезанным SVG. Клавиатурные кнопки/названия и подписи tabs присутствуют; axe на18 состояниях админки нарушений не нашёл.
- Gateway показывает текущий `/metrics` snapshot, historical ranges/SLO честно названы unavailable. Одновариантные Environment/Time range controls визуально похожи на действующие фильтры, хотя выбора нет; это кандидат на упрощение в шаге4, не отсутствующая backend-функция.
- Первичный Gateway/light390 поймал transient503 авторизации. Он сохранён как `light/390/gateway-error.png`, основной `gateway.png` переснят с успешными счётчиками. В остальных before-парах админка загружена. Ошибка не объявляется различием тем или подтверждённым постоянным дефектом.

## Г. Документация


| ID | Место и короткое утверждение | Что подтверждено | Предложение после согласования |
|---|---|---|---|
| DOC-01 | `README.md:3–9`: «with ... a SwiftUI app», «Status: foundation» | В `ui/` есть только `web`; ниже тот же README:45–46 уже пишет «SwiftUI client has not been implemented yet». Chat/Game/Calls есть в коде, поэтому foundation — устаревший текущий статус. | Вступление: Go auth/chat/game/gateway + web; SwiftUI отдельно в Planned. Заменить foundation на честную таблицу реализованных сквозных сценариев и границ проверки. Не заявлять production readiness. |
| DOC-02 | `CLAUDE.md:19`: список сервисов содержит `calling`; :27–30 прямо говорит, что отдельный сервис не создаётся | В `core/services/` только auth/chat/game/gateway. Calls собирается внутри Chat: `core/services/chat/cmd/chat/main.go:189–218`. Внешний контракт отделён в `core/shared/proto/axon/calls/`. ADR-1 и будущая граница: `.claude/plans/calls-hybrid.md:27–35`. | Удалить `calling` из списка действующих сервисов и дерева :106–109; сохранить ADR «Calls — модуль Chat» с условиями возможного будущего выделения. Не выделять процесс только ради совпадения с картинкой. Аналогичные изменения нужны AGENTS.md — он сейчас дубль. |
| DOC-03 | `CLAUDE.md:35–36`, дерево :121–141 описывают SwiftUI как существующий клиент; :60 — Quoridor как «игра в наборе» | `ui/app` отсутствует. В `core/services/game/internal/games/` только tictactoe; в `core/services/game/cmd/game/main.go:73–75` зарегистрирован только `tictactoe.Definition()`. `quoridor/chess/durak/checkers` встречаются в тестовых stub-ключах engine registry, что не является реализацией игр. | Убрать отсутствующие директории из дерева текущего состояния; идеи SwiftUI/Quoridor/прочих игр перенести в явно обозначенный Planned. BFS и стены — проектное решение на будущее, а не описание выполненного кода. |
| DOC-04 | `docs/schema.jpg` (просмотрено изображение): Desktop+Mobile SwiftUI, Calling Service, пять игр, стрелки WebRTC через Gateway | Это исходный концептуальный рисунок, а не фактическая схема. Calls живёт в Chat, SwiftUI/четырёх игр нет. Media клиент↔peer/SFU не проходит через gateway; gateway обслуживает RPC/сигналинг и HTTP/WSS control, не RTP. | **docs/archive** для оригинального рисунка, подпись «исходная концепция, не текущая архитектура». После одобрения сделать маленькую актуальную схему из кода; не стирать оригинал и не тратить этап на косметическое редактирование растра. |
| DOC-05 | `docs/README.md:3`: «schema.jpg — the data model sketch the services are built against»; :4 ссылается на `architecture.md` | Картинка — высокоуровневая архитектурная концепция, не модель данных. `docs/architecture.md` в дереве отсутствует. `CLAUDE.md:45` ещё и утверждает, что schema.jpg лежит рядом с корневым файлом, хотя реальный путь — `docs/schema.jpg`. README:106–107 обещает будущую architecture.md. | Исправить назначение и путь рисунка; убрать битую ссылку либо создать краткую текущую architecture.md после согласования, с ADR Calls и таблицей implemented/planned. |
| DOC-06 | `CLAUDE.md:22`: Kafka — межсервисные события `game.started`, `chat.message`; на схеме Kafka общее связующее звено | Неверно говорить, что Kafka вообще не реализована. Chat реально создаёт producer в `cmd/chat/main.go:392–412`, публикует `chat.message` в `internal/events/events.go:16,49–73`; `internal/service/service.go:435,543` вызывает publisher. Consumer-библиотека есть в shared. В сервисах потребителей Kafka и публикации game.started/game.finished не обнаружено; roadmap:214 это уже честно относит к будущей работе. При пустой конфигурации brokers Chat явно использует discard (:393–395). | Написать точную границу: «Chat публикует chat.message при настроенных brokers; межсервисные потребители и game/call events запланированы». Не удалять Kafka-пакет как мёртвую заготовку на основании неполной схемы. |
| DOC-07 | `.claude/plans/handoff.md:3–7` называет себя «живым» текущим статусом, но дата 2026-08-10; :19 — Chat сервиса нет; :20 — Game session/server/proto нет; :24 — четыре модуля | Текущее `core/go.work:3–8` содержит пять модулей, Chat и Game server/store/proto присутствуют. :45–48 требует Docker-only, тогда как действующие `README.md:38–58`, `.claude/rules/infra.md:3–10` разрешают и описывают native dev launcher. | **docs/archive** как handoff конкретного снимка; убрать из текущих ссылок «где мы сейчас», оставить датированное историческое описание. Текущий launcher и review-in-Docker не смешивать. |
| DOC-08 | `.claude/plans/roadmap.md:8–10`: «репозиторий сейчас пустой»; :163 «транспорт готов, сервиса нет» | Это устаревший greenfield-контекст, причём дальше :185–195 уже перечислены реализованные возможности Chat; часть плана позже обновлена. | Архивировать старый roadmap. Если сохранять текущим — вынести исходный контекст в Historical, заменить статус Chat, однозначно отделить planned от implemented. Старые Calling bullets :239–245 уже помечены заменённым дизайном на :237: не представлять их как обман о текущем коде. |
| DOC-09 | `.claude/rules/mcp-tools.md:7–12` обещает Docker и GitHub MCP | В текущем `.mcp.json` определены только shadcn, postgres и playwright. Возможности активной сессии зависят ещё от хоста, поэтому этот файл не является достоверным перечнем инструментов каждого участника. | Локальную настройку и список инструментов исключить из Git либо оставить проверяемый пример. В общих правилах достаточно «предпочитать структурированный инструмент, если он доступен», без утверждения несуществующих подключений. |
| DOC-10 | `.claude/rules/comments.md:3–19`: запрещены даже `// why`, причина — экономия токенов агента | Это прямо расходится с текущим запросом пользователя: комментарий допустим, если объясняет неочевидное почему. Директивы компилятора/линтера перечислены отдельно как необходимые (:22 и далее). | Переписать правило: оставлять локальный контекст гонки/инварианта/внешнего ограничения; удалять пересказ кода. Директивы и юридические заголовки не удалять. Причину «токены агента» убрать из инженерного обоснования. |
| DOC-11 | `docs/calls.md:7,47,79,103` и `.claude/plans/calls-hybrid.md:3` — рубежи закрыты, physical Safari/iOS не проверен | В тех же документах границы Docker fake capture явно описаны. Позднее `docs/calls.md:17` и `docs/calls-safari-ios-runbook.md:60,75` фиксируют подтверждение владельца 13 ноября: Answer with audio, включение камеры, поворот, Return to call на iPhone. Это частичное подтверждение, не готовность всех устройств/сценариев. | Свести в одну датированную матрицу. Сохранить basic iPhone UX как «подтверждено владельцем 13.11, сегодня не повторялось», не писать, что вообще ничего никогда не проверялось. PiP/wake lock/ringtone/groups/handoff на телефонах оставить UNVERIFIED. Общие «hardware не проверено» уточнить по сценариям/исторической дате. |
| DOC-12 | `docs/calls.md:7,69,77` и `.claude/plans/calls-hybrid.md:3,6,10` ссылаются на абсолютные локальные пути вне репозитория | `.local` и внешние visualizations не входят в Git-снимок; такие ссылки не работают для читателя клона. Отсутствие переносимого артефакта не опровергает исторический прогон, но не позволяет независимо повторить/проверить вывод из репозитория. | Сократить текущие docs до устройства/ограничений/команд повторения. Датированные результаты в archive с commit/image/tool versions и очищенными переносимыми результатами или ссылкой на CI artifact с retention. Непереносимые ссылки пометить local-only, не выдавать за доступные публичному читателю. |
| DOC-13 | `docs/calls.md:154–156` описывает прежний профиль LAN DNS как актуальный; runbook уже переключён на Tailscale, а PROGRESS:1–8 описывает незакрытый DNS этап 17 ноября | Это два альтернативных исторических сетевых состояния и не подтверждённая текущая инструкция для телефона. Наличие сетевого блока сегодня само по себе не опровергает iPhone проверку 13 ноября. | После подтверждения текущего DNS оставить один актуальный runbook, старую LAN/Internet Sharing инструкцию в archive с датой. До проверки физического телефона явно указать, что инструкция доступа в текущей сети ещё не подтверждена. Не переносить в публичный текст машинные адреса/служебные значения из PROGRESS. |
| DOC-14 | `.claude/plans/chat-live-status.md:3`: «Статус: выполнено»; :112–128 перечисляет команды/ручные сценарии | Тестовые файлы и реализация перечислены по именам; сам список команд не равен логу конкретного прогона. Нельзя делать вывод «фича не реализована» только по отсутствию свежей отметки. | В archive сохранить как описание выполненного среза с датой/commit. Текущий статус проверок привязать к текущему CI/запуску аудита, не оставлять бессрочную гарантию «всё зелёное». |
| DOC-15 | `docs/reviews/2026-11-14-project-review.md:7–22`, `2026-11-15-fix-results.md:9–24`: исправления и «проверено» | Для этих отчётов в `docs/reviews/2026-11-14/evidence/` действительно есть очищенные логи/JSON; evidence/README:3–5 описывает происхождение и один упавший прогон с отдельной успешной перепроверкой. Границы Chromium/local-only/remote CI unrun честно указаны в отчётах :26 и :37. | Не удалять и не объявлять сфабрикованными. **docs/archive** целым набором; добавить точный snapshot/hash при последующей проверке, сохранить различие historical result vs current status. Production-ready из этих файлов не следует и ими не заявляется. |

### Что не подтвердилось из первоначальной формулировки

- Строка **`not reverified`** (также `not_reverified`, `not-reverified`) в `.claude/plans`, `docs` и PROGRESS.md не найдена. Есть исторические статусы и явно непроверенные сценарии, но буквальный маркер не подтверждён.
- Kafka — не просто декоративный блок на схеме: Chat publisher реализован и подключён. Будущие consumers/game events не следует объявлять существующими.
- «Все звонки на устройствах никогда не проверены» слишком широко: есть частичное подтверждение iPhone UX от владельца 13 ноября. PiP/wake lock/входящий звук/физические группы и handoff остаются непроверенными.
- Старые «сервис calling» bullets в roadmap уже явно отменены предшествующим абзацем; проблема в дублировании истории с текущими инструкциями и устаревшем рисунке, а не в скрытом действующем решении вынести сервис.
- `docs/reviews` уже содержит очищенные свидетельства. Наличие агентных имён в файлах, shadcn/Base UI и стандартных компонентов само по себе не доказывает дефект кода/дизайна.

### Предлагаемый объём будущей ручной проверки звонков (без кода)

| Сценарий | Что известно на сегодня | Что нужно зафиксировать на реальном устройстве |
|---|---|---|
| Базовый iPhone Answer with audio/Camera, поворот, Return to call | Подтверждено владельцем 13 ноября по docs; не повторено в этом аудите | Устройство/ОС/браузер, дата и повторное подтверждение после сетевых изменений |
| Picture-in-picture | `runbook:64,68`: результата на iPhone нет | Поддержка браузером, один звуковой путь, возврат в Axon, PiP закрывается на End |
| Wake lock | `runbook:65,68`: результата нет | Экран удерживается только во время включённой функции; background/возврат и End освобождают lock |
| Incoming call sound | `runbook:66,68`: результата нет | Один сигнал при входящем после жеста/разрешения, отсутствие двойного звука, foreground/background policy Safari |
| LiveKit группа на телефонах | `runbook:83–91`: hardware matrix не выполнена | Три физических устройства, звук/видео у всех, mute/camera, leave одного не обрывает остальных, End освобождает capture |
| P2P→SFU на железе | `runbook:87–91`: подтверждён только Docker/LAN transport gate | Сохранение Call ID, реальная слышимая пауза, нет двойного аудио, отказы и End во время handoff, прекращение захвата |

Перед архивированием сначала вынести действующие ADR (граница Calls, транзакции/ACL, transport ownership), команды запуска/отката и матрицу ограничений в короткие переносимые документы. Затем согласовать точные перемещения и exclusions, и только после этого менять пути. Текущий этап не требует реструктуризации сервисов ради совпадения с рисунком.


## Измеренное покрытие backend


Локально Go 1.27.4 (через автоматический toolchain selection из go.work), GOCACHE указывает на существующий `.local/cache/go-1.27.4`. Для каждого модуля `shared`, `auth`, `chat`, `gateway`, `game` выполнены:

```sh
go test -timeout 90s -coverprofile=/tmp/axon-quality-audit-2026-11-18/backend-<module>.cover ./...
go build ./...
go vet ./...
```

Все 15 запусков успешны. Read-only `gofmt -l` по Go исходникам и тестам, без generated: список пуст. Дополнительно golangci-lint v2.15.0 (версия из CI) установлен во временный bin и собран с Go1.27.4; полный прогон по пяти модулям завершился успешно: 0 issues. Первоначальная сборка CLI с Go1.26 не подходила target1.27.4, повторная сборка устранила несовместимость инструмента без изменения исходников.

Покрытие — сумма покрытых statements / всех statements из профиля, generated исключены. Это НЕ доля символов, не процент файлов и не среднее арифметическое package percentages. Включены cmd/config/repository без unit tests.

| Модуль | Statements covered/all | Покрытие |
|---|---:|---:|
| shared | 684 / 1001 | 68,33% |
| auth | 853 / 1825 | 46,74% |
| chat | 1747 / 6253 | 27,94% |
| gateway | 223 / 728 | 30,63% |
| game | 175 / 620 | 28,23% |

Показательные пакеты:

| Package | Покрытие |
|---|---:|
| auth/internal/service | 77,55% |
| auth/internal/service/refresh.go (файл) | 82,35% |
| auth/internal/service/session.go (файл) | 78,05% |
| auth/internal/service/rbac.go (файл) | 93,94% |
| auth/internal/service/avatar.go (файл) | 23,53% |
| auth/internal/repository и server | 0% |
| chat/internal/service | 79,70% |
| chat/internal/ws | 55,65% |
| chat/internal/calls | 40,39% |
| chat/internal/server | 0,71% |
| chat/internal/repository | 0,68% |
| gateway/internal/guard | 81,82% |
| gateway/internal/cookie | 92,50% |
| gateway/internal/policy | 100% |
| gateway/internal/ratelimit | 88,33% |
| gateway/internal/wsproxy | 84,38% |
| gateway/internal/proxy и HTTP object proxies | 0% |
| game/internal/engine | 100% |
| game/internal/games/tictactoe | 87,50% |
| game/internal/session | 9,05% |

Ограничения: только default build tags; integration/e2e с Postgres не запускались; `-race` не запускался. Generated protobuf исключён из подсчёта. Нулевое unit-покрытие repository не доказывает отсутствие существующих integration-сценариев. Никакие проценты не заменяют проверку физических устройств для звонков. Это исходное измерение для будущей работы, не итог шага 2.

Краткие машиночитаемые результаты сохранены в `docs/design/before/backend-{checks,vet,size,coverage,compose-result}.json`. Полные cover profiles и логи остались локально в `/tmp/axon-quality-audit-2026-11-18/`, в репозиторий не включены. Команды выше позволяют воспроизвести измерение.

## Проверки веба, доступности и снимков

- `npm run test`: 19/19 файлов, 92/92 теста PASS.
- `npm run lint`: 0 ошибок, 3 прежних предупреждения React Compiler о TanStack Virtual (`call-history-sidebar`, `directory-list`, `room-list`). Предупреждения не замалчиваются как «линтер полностью чист».
- `npm run typecheck`: PASS.
- `npm run build`: exit1 из-за security check Next.js, подробности B10. Новый Docker build/deploy на этапе инвентаризации не выполнялся.
- Снята матрица `390/768/1440 × light/dark` для login, register, forgot-password, пустого chat, заполненного conversation, contacts, calls, media, profile, appearance, sessions, notifications, games, tic-tac-toe lobby и practice: **108 основных PNG +6 кадров skip-link дефекта +1 transient Gateway error-state, всего115 PNG**. Существующие тестовые аккаунты, без создания сообщений/звонков/партий. Sessions и Notifications пересняты после окончания skeleton/loading. Высота viewport900, fullPage PNG; это проверка ширины, не доказательство поведения на физическом телефоне.
- В проверенных DOM нет горизонтального overflow и `img` без alt. Пустой alt внутри уже подписанной media-кнопки сам по себе не дефект. Заполненный чат проверен отдельно от пустого состояния.
- Axe-core4.14.0 был уже установлен: существующий инструмент, новые зависимости не добавлялись. Проверены WCAG2 A/AA,2.1 AA,2.2 AA rules на основной матрице и отдельных игровых/чатовых состояниях. Все108 основных page/theme/width состояний проверены, в стабильном состоянии violations0; есть `incomplete` (фоновые градиенты/ограничения определения background и отдельные aria-атрибуты). Это **не полная сертификация WCAG AA**.
- При мгновенной смене темы axe однажды измерил старый foreground на новом фоне. После ожидания финального computed color в обеих темах повторная проверка заполненного чата и регистрации — без нарушений. Переходный замер не записан как постоянный contrast-дефект.
- [WCAG1.4.3](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html): для обычного текста проверяется4.5:1, для крупного3:1; disabled/decorative элементы не объявляются ошибками по этому критерию. Автоматический текстовый contrast-check не проверяет все non-text boundaries и не заменяет ручной разбор градиентов.
- Клавиатура: command palette получает фокус, после Escape и завершения закрытия возвращает его на исходную Chats-ссылку; message-actions через Shift+F10 открывает Reply, Escape возвращает фокус; ArrowRight переносит фокус между клетками игры. Первичный немедленный замер до завершения close-animation не выдаётся за ошибку возврата фокуса.
- DOM Appearance: при dark все4 preview-секции (`color-scheme: light/dark`) имеют один bg `rgb(14,18,27)` и одинаковые цвета дочерних штрихов. Визуально светлая тема даёт аналогично одинаковые светлые previews. VIS-01 подтверждён данными, а не субъективной оценкой.
- DOM Register1440: обычные Email/Display поля43px, внутренний Username input30px, с рамкой wrapper около32px; Password input41px внутри43px wrapper. Неодинаковая высота VIS-02 подтверждена.
- Все9 клеток Practice имеют одинаковые размеры:390 —114×114px,768/1440 — примерно121.33×121.33px с округлением долей CSS-пикселя. Визуальное подозрение о разных рядах не подтвердилось.
- Во время серии полных перезагрузок наблюдались429 на ListSessions/RegisterEndpoint с последующим успешным retry200. Это нагрузка аудита; по этому прогону не утверждается, что обычная навигация всегда ломается. Rate limit стенда не повышался. Кадры с временными skeleton не используются как доказательство окончательного вида Sessions/Notifications.
- `Current session / Offline` наблюдалось во время переподключения; после восстановления присутствия стало Online. Не записано как доказанный серверный дефект. Можно отдельно улучшить терминологию active vs online.
- Админка проверена под отдельным тестовым admin; активная сцена звонка и физические iPhone/SFU/PiP/wake-lock/audio сценарии не проверялись. Роль admin назначена только новому разрешённому тестовому аккаунту; push subscription и медиа-разрешения не назначались. Credentials сохранены вне репозитория в локальном файле с mode0600.

## Следующий согласуемый порядок

1. Согласовать этот список. До одобрения — никаких исходных исправлений/перемещений.
2. Проверенный patch-update Next.js; для web-e2e Compose достаточно оставить уже существующий game и закрепить конфигурацию проверкой. Для найденного auth reuse→WS revocation сначала воспроизвести регрессионным тестом и исправить в согласованном объёме.
3. Разделить организационные обязанности Chat внутри текущего контракта; если требуется смена сервиса/транзакций — представить отдельный план и остановиться.
4. Решить, оправдана ли генерация RPC wrappers; новый общий helper поверх уже существующего не добавлять автоматически.
5. Разделить ChatRoomView по сценариям и добавить тесты пользовательского поведения компонентов; затем недостающие backend tests по реальным рискам границ.
6. Чистка А/Б/Г только по утверждённым путям. Дизайн — отдельные2–3 направления и ожидание выбора на шаге4. Пока направления и палитры не выбирались.
7. Заметки в docs/notes и ревью полного диффа относятся к последующим этапам, сейчас не заменяют их отчётом об инвентаризации.
