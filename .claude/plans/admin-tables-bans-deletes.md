# Админка: таблицы, баны, правка и мягкое удаление

## Контекст
После #61 и #62 у консоли есть списки и карточки записей, но владелец (2026-11-22) недоволен:
- таблицы листаются только бесконечной прокруткой;
- сортировка есть только у users, фильтров по колонкам нет;
- чекбоксы, селекты и поля дат нативные и выбиваются из темы;
- при действиях экран прыгает (панель массовых действий, инлайн-формы);
- нет правки и удаления записей, нет банов;
- отступы и высоты контролов разъезжаются от страницы к странице.

Аудит со скриншотами (десктоп 1440, мобильный 375, 22 страницы) подтвердил:
- панель массовых действий сдвигает таблицу на 45 px (на мобильном на 79 px);
- инлайн-форма турнира сдвигает контент на 422 px;
- tap-таргеты на мобильном 28–32 px;
- на audit на мобильном двойной скролл;
- колонка Remove в турнире уезжает за край на мобильном;
- контролы разной высоты (32 px и 44 px);
- в шапке видны сырые ETag;
- в gateway/service-map неверная подпись «Back to profile».

Решения владельца:
- **Баны** — со сроком и по сервисам: блокировка учётки, мут в чатах, запрет матчмейкинга, а также бан по email и IP.
- **Удаление** — мягкое, с восстановлением 30 дней.
- **PR** — #62 смержен. Дальше отдельные PR от main, по одному на этап.

Результат: каждая админская таблица постранична на десктопе и бесконечна на мобильном, сортируется и фильтруется по колонкам, а контролы общие и тёмные. Банить, править и мягко удалять можно из списка и из карточки, всё пишется в аудит. Ни одно действие не сдвигает контент.

## Этап 1 — таблицы и контролы (`feat/admin-tables-and-controls`)

### Контракт списков
Во все админские `List*` добавляются аддитивные поля:
- `string filter` — подмножество AIP-160: `field op value`, `AND`, `OR` (связывает сильнее), `NOT`, скобки; операторы `= != < <= > >= :`; значения — строки в кавычках, числа, RFC 3339, enum; не больше 1000 символов, 24 сравнений и глубины 4;
- `string order_by` — AIP-132: `field [desc], ...`;
- `int32 offset`.

В ответе `int64 total` есть везде. Лимиты protovalidate: `filter` ≤ 1000, `order_by` ≤ 200, `offset` ≤ 100 000. Поле `cursor` остаётся рабочим, но админка переходит на `offset`: при сортировке по любой колонке keyset-курсор пришлось бы кодировать на каждую колонку, а объёмы админских выборок это позволяют.

Общий пакет `core/shared/pkg/adminquery`:
- схема ресурса: поле → SQL-выражение, тип (string, uuid, enum, int, time, bool), допустимые операторы, сортируемость;
- парсер `filter` и `order_by`;
- сборка `WHERE`/`ORDER BY`/`LIMIT OFFSET` с параметрами;
- ошибки разбора → `InvalidArgument` с `field_violations`.

Подключение по сервисам:
- auth: `ListUsers`, `ListAuditEvents`;
- chat: `ListChatRooms`, `ListChatUploads`, `ListAllCalls`;
- game: `ListGameSessions`, `ListMatchQueue`;
- rating: `ListRatings`, `ListRatingChanges`;
- analysis: `ListAnalysisReports`;
- tournament: новый `ListAdminTournaments` в `TournamentAdminService` (право `games:read`, в gateway — тир поиска); работает только постранично, `total` приходит всегда.

`ListAdminRoles` остаётся без пагинации: ролей десятки, их сортирует и фильтрует клиент.

### Таблица (`ui/admin/src/components/admin/data-table.tsx`)
- **Десктоп (≥ 768 px):**
  - пагинация: «1–25 of 11 017», первая, назад, номер страницы, вперёд, последняя, размер страницы 25/50/100;
  - страница и размер хранятся в URL;
  - высота таблицы фиксирована по `pageSize`, поэтому при смене страницы ничего не прыгает.
- **Мобильный:**
  - бесконечная подгрузка по индексу последней видимой строки виртуализатора;
  - строки — карточки (`meta.mobile` у колонки: title, subtitle, meta, action) вместо широкой таблицы во вложенном скролле.
- **Заголовок колонки — поповер (Base UI Popover):** сортировка по возрастанию, по убыванию, сброс; фильтр по типу колонки (у колонки может быть несколько фильтров, например Members: участник и число участников):
  - текст — «содержит»;
  - enum — множественный выбор;
  - число и дата — диапазон;
  - пользователь — поиск по имени вместо UUID;
  - идентификатор — точное совпадение по полному UUID.

  Активные фильтры показываются чипами в тулбаре с «Clear all». На мобильном фильтры открываются в полноэкранном `Sheet` с сортировкой, а в тулбаре остаются поиск и кнопка «Filters · n». Состояние списка в URL: `page`, `size`, `sort` (`-field` — по убыванию), `f.<поле>`.
- **Действия со строкой:**
  - меню «⋯» в последней колонке (Open, Edit, Delete, Ban — по ресурсу);
  - чекбоксы только там, где есть массовые действия (users, games, сообщения чата); у прежнего нативного чекбокса в games не было видимого состояния, новый его показывает.
- **Панель массовых действий** плавает поверх нижнего края таблицы (absolute, тень) и не участвует в потоке. Сдвиг 0 px.

### Контролы (`ui/admin/src/components/ui/`)
- `checkbox.tsx` — Base UI Checkbox с indeterminate. Заменяет нативные чекбоксы в data-table, user-dialogs, permission-picker.
- `select.tsx` — Base UI Select. Заменяет `<select>` в `SelectFilter`, gateway, service-map.
- `number-field.tsx` — Base UI NumberField со степперами. Используется в форме турнира.
- `date-range.tsx` — редактор фильтра дат с пресетами (Today, Last 7 days, Last 30 days, This month) и своей сеткой месяца без новых зависимостей. Заменяет `input[type=date]` в audit.
- Единая шкала высот: 32 px на десктопе, `pointer-coarse:min-h-11` у всех кнопок, полей, селектов и пунктов меню. Иконочные кнопки на мобильном тоже 44 px.

### Сдвиги и раскладка
- Правка записи открывается в `Sheet` (`components/ui/sheet.tsx`): справа на десктопе, во весь экран на мобильном. Инлайн-форма турнира уходит туда же; у `ResourceForm` есть липкая панель кнопок (`sticky`).
- `RequirePermission`/`RequireAdmin` пока грузятся, рисуют каркас страницы (шапка и скелетон) вместо голого «Checking access…».
- `scrollbar-gutter: stable` у скролл-контейнеров.
- Подсказка и ошибка поля делят одну строку фиксированной высоты — не сделано в этапе 1.
- Форма роли: ширина колонки записи и липкая панель Save/Cancel.

### Мелкие правки из аудита
- `<main>` у списков.
- Одинаковый отступ тулбара и таблицы.
- Радиус 12 (`rounded-xl`) у карточек, секций панели, форм и рамок записей.
- Version в фактах убран: ETag непрозрачный, номера версии в API нет.
- Подпись «Back to administration» в gateway/service-map.
- Ratings: Standings/Changes переделать в Tabs.
- Calls: тулбар с фильтрами скрыт, когда звонки выключены.
- Roles: сортировка и фильтры на клиенте (`localRows` в `lib/admin/list-query.ts`).
- Порядок блоков в карточке на мобильном: шапка → содержимое → факты.

### Чекбокс в `ui/web` (отдельный PR `feat/web-checkbox`)
Тот же `checkbox.tsx` в `ui/web/src/components/ui/`. Замены в `profile/notifications.tsx` и `calls/call-preferences.tsx`.

## Этап 2 — баны (`feat/admin-bans`)

### Данные
Миграция auth `000010_bans`. Таблица `auth.bans`:
- `id`;
- `scope`: `account`, `chat`, `matchmaking`;
- `subject`: `user`, `email`, `ip`;
- `user_id`, `email` (точный адрес или `*@domain`), `ip_range` (`cidr`);
- `reason`, `created_by`, `created_at`, `expires_at` (NULL — навсегда);
- `lifted_at`, `lifted_by`, `lift_reason`;
- `version`.

Ограничения:
- бан по email и IP бывает только `account`;
- бан по `user` может быть любой области.

Текущие блокировки (`users.suspended_*`) переносятся в бессрочные `account`-баны. Колонки остаются проекцией активного бана.

### Применение
- **auth.** Активный бан учётки — `expires_at IS NULL OR expires_at > now()`, джоб для снятия не нужен. Вход, refresh и выдача токена отказывают при:
  - активном `account`-бане пользователя;
  - совпадении email с баном;
  - совпадении IP клиента (`X-Axon-Client-Ip` от gateway) с баном.

  Регистрация и создание учётки через OAuth отказывают по email или IP. Бан учётки отзывает сессии, как сейчас Suspend.
- **chat и game.** auth пишет в `auth.outbox` событие `auth.restriction.changed.v1` (`user_id`, `scope`, `expires_at`, `active`). Схема — в `axon.events.v1`.
  - chat держит `chat.user_restrictions`, `Send` отвечает `PermissionDenied` «muted until …».
  - game держит `game.user_restrictions`. `StartMatch`, `CreateSession` и `JoinSession` отвечают так же.
  - Consumer-группы — по сервису.
- **Известный предел.** Уже выданный access-токен живёт до 15 минут. Для WebSocket чата сессии закрываются сразу через revocation.

### API и права
`AuthAdminService`:
- `ListBans` — с filter/order_by/offset;
- `GetBan`;
- `CreateBan` — с `request_id`;
- `LiftBan` — с etag и причиной.

Права:
- `account`, `email`, `ip` — `admin.users.mutate`;
- `chat` — `admin.chat.moderate`;
- `matchmaking` — `admin.games.moderate`.

Правила попадают в `AdminRules` и в policy gateway (TierAdmin). `SuspendUser`/`RestoreUser` остаются для совместимости и работают через баны.

### UI
- Раздел **Bans** в группе People: список с субъектом, областью, причиной, автором, сроком и статусом (Active / Expired / Lifted), диалог создания и снятие бана.
- **Карточка пользователя:** кнопка «Ban…» вместо Suspend. Диалог с областями, сроком (1 h, 1 d, 7 d, 30 d, навсегда, своя дата) и причиной. Секция «Restrictions» с активными банами и историей.
- **Список пользователей:** колонка Status показывает «Banned until …», «Muted», «No matchmaking», иначе приглушённое «Active».

## Этап 3 — правка и мягкое удаление (`feat/admin-edit-delete`)

### Общий механизм
- Колонки `deleted_at`, `deleted_by`, `delete_reason`.
- Пользовательские запросы фильтруют удалённое.
- В админских списках фильтр `state = deleted` и действие Restore.
- Каждый сервис раз в час чистит записи старше 30 дней (`admincrud.Purge` с аудитом).

### По сущностям
- **Users.**
  - Правка: имя, username, email. Смена email снимает подтверждение; уникальность возвращается ошибкой на поле.
  - Удаление: вход запрещён, сессии отозваны, профиль скрыт из поиска.
  - Через 30 дней чистка анонимизирует PII: email `deleted-<id>@axon.invalid`, имена пустые, аватар удалён, OAuth отвязан.
- **Roles.** Удаление становится мягким: роль скрыта из каталога и не выдаётся.
- **Tournaments.** Удалять можно в статусах registration, cancelled и finished. Удалённый турнир скрыт из публичных списков.
- **Chats.**
  - Группу можно переименовать и удалить: участники теряют доступ.
  - Админское удаление сообщения сохраняет тело в `chat.message_moderation` на 30 дней, чтобы его можно было восстановить.
- **Uploads, analysis reports.** Мягкое удаление. Объект в MinIO удаляется при чистке.
- **Games.**
  - Правки нет: это журнал.
  - «Void result» для завершённой рейтинговой партии: событие `game.session.voided.v1`, rating пишет компенсирующее изменение (−delta), партия помечается аннулированной.
- **Ratings.** «Adjust rating» — ручное компенсирующее изменение с причиной.
- **Audit.** Только чтение.

### UI
Edit и Delete доступны:
- в меню «⋯» строки;
- в шапке карточки.

Правка открывается в `EditSheet` поверх существующего кита форм (`update_mask`, etag, ошибки на полях). Удаление подтверждается с причиной. Фильтр Deleted и Restore есть в каждом списке.

## Затрагиваемые файлы (основное)
- `core/shared/proto/axon/{auth,chat,calls,game,rating,analysis,tournament}/v1/admin.proto`
- `core/shared/pkg/adminquery/` — новый пакет
- `core/services/*/internal/{repository,store,session}/admin*.go` — сборка запросов через adminquery
- `core/services/auth/migrations/000010_bans.*`, затем `00001x_soft_delete.*`; миграции soft delete в chat, tournament, analysis
- `core/shared/proto/axon/events/v1/events.proto` — `auth.restriction.changed.v1`, `game.session.voided.v1`
- `ui/admin/src/components/admin/data-table.tsx`, `filters.tsx`, `bulk-bar.tsx`, все `*-table.tsx`
- `ui/admin/src/components/ui/{checkbox,select,number-field,date-range,drawer,sheet}.tsx`
- `ui/admin/src/lib/admin/{query-state,filter}.ts` — сериализация фильтров и сортировки в URL и в AIP-160
- `ui/web/src/components/ui/checkbox.tsx`

## Как проверяется
- `cd core/shared/pkg/adminquery && go test ./...` — разбор и SQL по таблице случаев, включая инъекции в значениях и неизвестные поля.
- `go test ./...` в затронутых сервисах. `make test-e2e`: auth (баны, вход отклонён, снятие), chat (мут), game (запрет матча), списки с `filter`/`order_by`/`offset`.
- `ui/admin`: `npx tsc --noEmit`, `npx eslint src`, `npx vitest run` — сериализация фильтров и разбивка на страницы.
- Playwright `e2e/admin.spec.ts`: пагинация на десктопе; фильтр колонки и сортировка; бан и снятие; удаление и восстановление.
- Живьём на `axon-console`, 1440 и 375. Скриншоты каждой страницы. Замер сдвига `getBoundingClientRect` до и после выбора строк, открытия правки и смены страницы — 0 px. Tap-таргеты ≥ 44 px на мобильном.

## Критерий готовности
Все три этапа смержены. На axon.test любая таблица админки листается страницами на ПК, сортируется и фильтруется по колонкам, а пользователя можно забанить по области и сроку. Запись можно поправить и мягко удалить с восстановлением, и ни одно действие не сдвигает экран.
