# Админка: отдельное приложение и CRUD по сущностям

## Контекст
Первая версия консоли (`.claude/plans/web-admin.md`) живёт внутри игрового клиента — route group
`ui/web/src/app/(app)/admin` в общем shell — и умеет только списки и точечные команды. Владелец хочет
отдельный фронт со своим layout и CRUD на каждую сущность. Исследование
(`reports/Хорошие CRUD в отдельной админке.md`) показало: прямой CRUD по таблицам в Axon ломает
журнал партий, outbox и аудит, поэтому «CRUD на каждую таблицу» реализуется как режим на каждую
таблицу: полный CRUD там, где запись — это просто состояние без подписчиков, команды с причиной — для
сущностей с жизненным циклом, только чтение — для журналов, леджеров и проекций.

Результат: `axon.test/admin` отдаёт отдельное Next.js-приложение `ui/admin`, у каждой таблицы из схем
сервисов есть раздел с режимом из таблицы ниже, формы валидируются правилами из proto, конфликт версий
и ошибки на полях показываются в форме, каждая правка пишет в аудит diff.

## Режим по таблицам

| Таблицы | Режим | RPC (новые — жирным) |
|---|---|---|
| `auth.roles`, `auth.role_permissions` | CRUD (кроме системных `user`/`moderator`/`admin`) | **ListRoles, GetRole, CreateRole, UpdateRole, DeleteRole** в `AuthAdminService` |
| `auth.permissions` | чтение | **ListPermissions** |
| `auth.users`, `auth.user_roles` | чтение + команды | Suspend/Restore/RevokeSessions; **UpdateUserProfile** (`display_name`, маска + etag), **SetUserRoles** |
| `auth.oauth_accounts`, `auth.refresh_tokens` | чтение + команды | на карточке пользователя; **UnlinkOAuthAccount**, отзыв сессий |
| `auth.audit_log` | чтение + экспорт | ListAuditEvents; **ExportAuditEvents** (server-streaming) |
| `chat.rooms`, `chat.room_members` | чтение + команды | есть List/Get/RemoveMember |
| `chat.messages` | чтение по причине + команда | есть List/Delete |
| `chat.uploads`, `chat.files` | чтение + команда | **ListChatUploads, DeleteChatUpload** |
| `chat.calls*` | чтение + команда | есть ListAllCalls/TerminateCall |
| `game.sessions`, `game.moves` | чтение + команда | есть List/Cancel; **GetGameSession** с ходами |
| `game.match_queue` | чтение + команда | **ListMatchQueue, RemoveFromMatchQueue** |
| `rating.ratings`, `rating.changes`, `game.ratings` | чтение | **ListRatings**; есть ListRatingChanges |
| `tournament.tournaments` | CRUD до старта, потом команды | Create/Start/Cancel есть; **UpdateTournament** (только в `registration`, маска + etag) |
| `tournament.registrations` | чтение + команда | **RemoveTournamentPlayer** (до старта) |
| `tournament.rounds`, `matches` | чтение | в `GetTournament` |
| `analysis.reports` | чтение + команды | **ListAnalysisReports, RerunAnalysis, DeleteAnalysisReport** в новом `AnalysisAdminService` |
| служебные (`outbox`, `processed_events`, `owners`, `projector_offsets`, `signing_keys`, `call_outbox`, `upload_sessions`, `deadlines`) | не показываются | инфраструктура, не доменные данные |

Удаление пользователя (GDPR, асинхронно по всем сервисам), ручная правка рейтинга (`AdjustRating`),
импорт и отдельная админская сессия со step-up — вне этого плана, отдельные задачи.

## Подход

### 1. Отдельное приложение `ui/admin`
- Next.js той же версии, что `ui/web`, `basePath: "/admin"`, `output: "standalone"`, свой layout
  (боковая навигация по ресурсам, плотные таблицы), без игрового shell. Строгий nonce-CSP с
  `connect-src` только на gateway — вся админка динамическая, кэш ей не нужен.
- Перенос из `ui/web`: `src/components/admin/*`, `src/lib/admin/*`, `src/lib/query/admin.ts`,
  страницы `src/app/(app)/admin/**`, `e2e/admin.spec.ts`. В `ui/web` route group удаляется, ссылка
  «Администрирование» становится `<a href="/admin">`.
- Сессия: та же refresh-cookie gateway, при старте `RefreshToken`, как в игровом клиенте. В `ui/admin`
  — минимальные `lib/connect/transport.ts` и `lib/auth/session.tsx` (bearer-интерсептор + refresh).
  Общий workspace-пакет отвергнут: потребовал бы переделки `Dockerfile.web` и CI ради ~150 строк.
- shadcn-компоненты ставятся в `ui/admin` через shadcn CLI (`components.json`), как и в web.
- Генерация: второй `out: ui/admin/src/gen` у плагина `bufbuild/es` в `buf.gen.yaml`,
  `/ui/admin/src/gen/` в `.gitignore`.
- Маршрутизация: в `ui/web/next.config.ts` `rewrites` `/admin` и `/admin/:path*` на `ADMIN_WEB_URL`
  (multi-zones). Origin остаётся прежним — `CORS_ALLOWED_ORIGINS` и `calls-lan-proxy.mjs` не меняются.
- Обвязка: сервис `admin-web` в `core/deploy/docker-compose.yml` (сеть `edge`, `Dockerfile.web` с
  контекстом `../../ui/admin`), `ADMIN_WEB_URL=http://admin-web:3000` у `web`; то же в
  `docker-compose.web-e2e.yml`; `DEV_ADMIN_PORT` в `scripts/start-dev.mjs`; чарт
  `core/deploy/k8s/charts/admin-web` и env у `web`; job сборки образа в CI.

### 2. Соглашения контракта и общий серверный пакет
- `buf.yaml`: зависимости `buf.build/bufbuild/protovalidate` и `buf.build/googleapis/googleapis`.
- В админских proto: правила `buf.validate`, `google.api.field_behavior` (`OUTPUT_ONLY`, `IMMUTABLE`),
  `google.protobuf.FieldMask update_mask`, `string etag`, `bool validate_only`, `string request_id`,
  `string reason`. Всё только новыми полями и сообщениями — additive-only сохраняется.
- Сервер: интерсептор `connectrpc.com/validate` (`NewInterceptor`, connect-go v1) на админских
  хендлерах; для Update — явная валидация ресурса после применения маски.
- `core/shared/pkg/admincrud`:
  - `ApplyMask(dst, src, mask, allowed map[path]authz.Permission)` — путь вне карты или без права →
    `PERMISSION_DENIED` с `ErrorInfo{reason: FIELD_PERMISSION_DENIED}`;
  - `Etag(version)` / `ParseEtag` → `W/"<n>"`; несовпадение → `ABORTED` с текущим etag в метаданных;
  - `Diff(before, after proto.Message, mask)` через `protoreflect`, поля с опцией `sensitive` →
    `[REDACTED]`;
  - `Violations(err)` — доменные ошибки (`23505` на уникальном индексе и т.п.) в `buf.validate.Violations`
    + `ErrorInfo`;
  - идемпотентность: таблица `<schema>.admin_requests (request_id, method, request_hash, response)`,
    запись в той же транзакции, повтор → сохранённый ответ, другой hash → `INVALID_ARGUMENT`.
- Аудит: `AdminAction` получает поле `changes` (diff), `auth.audit_log` — колонку `changes jsonb`
  (миграция auth `000009`), страница аудита показывает diff.

### 3. Кит ресурсов в `ui/admin` и роли как первый полный CRUD
- `src/resources/<name>.ts`: `name`, `recordTitle(row)`, `rowId(row)`, права, `list` (RPC, колонки,
  фильтры, сортировка), `detail` (секции, связанные списки), `form?` (схема из proto, поля, read-only
  правило), `actions` (тяжесть, причина, право, текст подтверждения). Маршруты
  `/admin/<name>`, `/admin/<name>/new`, `/admin/<name>/[id]`, `/admin/<name>/[id]/edit` — только если
  у ресурса есть соответствующие RPC.
- Общие компоненты: `ResourceList` (текущий `DataTable` + `useUrlState`, `getRowId`, три пустых
  состояния), `ResourceDetail`, `ResourceForm` (TanStack Form + `createStandardSchema` из
  `@bufbuild/protovalidate`, серверные `Violations` → ошибки полей через нормализатор snake_case →
  camelCase, панель конфликта на `ABORTED` с сохранением черновика, guard несохранённых правок),
  `ActionDialog` (текущий `ConfirmAction`), `ReferenceLink`/`ReferencePicker`, CSV-экспорт видимого
  списка с экранированием `= + - @`.
- Права в UI: раздел без `read` скрыт, недоступное действие выключено с причиной в тултипе.
- Роли: миграция auth `000009` добавляет `roles.title`, `roles.system`, `roles.version`,
  `roles.updated_at`; системные роли помечены и только читаются; удаление роли с участниками →
  `FAILED_PRECONDITION`, удалённая строка копируется в `auth.deleted_records`.

### 4. Турниры
- `UpdateTournament` (`name`, `capacity`, `rounds`, `no_show_seconds`) только в `registration`,
  `version` в `tournament.tournaments`; `RemoveTournamentPlayer` с причиной; форма создания в админке
  вызывает существующий `CreateTournament`. Cancel с `validate_only` возвращает число затронутых партий
  для текста диалога.

### 5. Команды и чтение по остальным таблицам
- По строкам таблицы режимов: users (`UpdateUserProfile`, `SetUserRoles`, `UnlinkOAuthAccount`),
  chat uploads, game (`GetGameSession` с ходами, match queue), rating (`ListRatings`), analysis
  (`AnalysisAdminService` с authz-кэшем как у остальных сервисов).
- Новые права — миграцией auth и в `core/shared/pkg/authz/permission.go`, каждый RPC — в карту
  procedure → permission, deny by default.

### 6. Экспорт аудита и массовые действия
- `ExportAuditEvents` (server-streaming, фильтр как у List, лимит 10 000 строк, снимок в
  `REPEATABLE READ`), сам экспорт пишется в аудит.
- Массовые действия на списках пользователей, партий и сообщений: клиентский цикл одиночных команд
  с параллелизмом 4, свой `request_id` на каждую, итоговый тост с группировкой отказов по причине,
  лимит выбора 100.

## Расхождения с замыслом
- `google.api.field_behavior` не используется: зависимость googleapis в buf не подключали, неизменяемость полей держится маской (`admincrud.Fields`) и сервером.
- Экспорт аудита — unary RPC с CSV до 10 000 строк (`truncated` в ответе), а не стрим: для админки этого хватает, стрим через gateway и Connect-web усложняет клиента.
- Таблицы `admin_requests` нет: идемпотентность создания роли — `request_id` становится id роли, повтор правки распознаётся сравнением текущего и запрошенного состояния.
- Загрузки чата и разборы партий только читаются: файлы дедуплицируются по sha и делятся между сообщениями, а повторный разбор невозможен — ходы приходят только из события `game.finished`.
- Отмена турнира без `validate_only`; правка турнира — только в статусе регистрации, версия турнира растёт при любом изменении строки (регистрации тоже), поэтому etag ловит и гонку с записью игроков.
- Массовые действия — клиентский цикл (4 параллельно, до 100 записей) по одиночным RPC: на каждую запись своя строка аудита, отдельного bulk-RPC нет. На `RESOURCE_EXHAUSTED` цикл ждёт `Retry-After` и повторяет запись (до 3 попыток).
- Админские записи вынесены из sensitive-тира gateway (10/мин, burst 5 — его съедал первый же массовый проход и e2e роли) в отдельный тир `TierAdmin`: `RATE_LIMIT_ADMIN_PER_MINUTE` (120 по умолчанию), burst 20, свой счётчик на пользователя. Экспорт аудита остался в sensitive — он читает до 10 000 строк.
- shadcn-компоненты в `ui/admin` скопированы из `ui/web`, а не поставлены CLI: так обе зоны остаются визуально одинаковыми.

## Как проверяется
- `make proto && buf lint && buf breaking --against '.git#branch=origin/main'` — без ошибок.
- `cd core && go test ./...` по затронутым сервисам и `core/shared/pkg/admincrud`; integration-тесты
  auth/tournament/analysis на роли, конфликт версий (`ABORTED`), идемпотентность, diff в `audit_log`.
- Go e2e (`make test-e2e`): CRUD роли через gateway, двойной Update с устаревшим etag → `ABORTED`,
  `validate_only` ничего не меняет, экспорт CSV отвечает через gateway. Diff в аудите здесь не
  проверяется: в `docker-compose.e2e.yml` нет Kafka и Debezium, аудит auth доезжает через outbox —
  его проверяют integration-тест auth и Playwright на `docker-compose.web-e2e.yml`.
- `cd ui/admin && npm run lint && npx tsc --noEmit && npx vitest run`; `cd ui/web` — то же.
- Playwright (`docker-compose.web-e2e.yml`): создать роль → правка → конфликт из второй вкладки →
  удаление; ошибка на поле при невалидном имени; `/admin` с основного origin открывает отдельное
  приложение; существующий `admin.spec.ts` зелёный.
- Живьём на одноразовом стенде `axon-admin` (`.local/admin-stand/compose.sh`): каждый раздел из
  таблицы режимов открывается, команды пишут аудит с diff; после `/ship` — деплой на общий стенд
  `axon.test/admin` по согласию владельца.

## Критерий готовности
`/admin` на Docker-стенде отдаётся отдельным приложением `ui/admin`, у каждой таблицы из таблицы
режимов есть раздел с указанным режимом, и все проверки выше проходят.
