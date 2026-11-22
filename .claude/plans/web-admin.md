# Web: админка для всех сервисов

## Контекст

Сейчас админка — три страницы в `ui/web/src/app/(app)/admin/`: поиск аккаунта по точному
email с назначением ролей (`roles-manager.tsx`), дашборд gateway по `/metrics` и карта
сервисов. Доступ проверяет только клиентский guard `useRequireRole("admin")` и сам auth на
своих RBAC-ручках; chat, game, rating ничего не знают о ролях, tournament спрашивает `GetMe`.
Аудит есть только у auth (`SecurityEvent` в `auth.audit.v1` через outbox), прочитать его
из UI нельзя.

Задача владельца: «админку хорошую ... для всех». Результат — одна админка, через которую
модератор и админ управляют пользователями, партиями, турнирами, чатами и звонками; каждое
действие проверяется правом в самом сервисе и попадает в общий журнал, который виден в UI.

Ресерч (deep-research, отчёт «Практики хорошей веб админки» в заметках сессии) дал опорные
правила, по ним и строится план:
- навигация по ресурсам, а не по сервисам; обзор с тем, что требует внимания; у каждой
  записи своя ссылка `/admin/<resource>/<id>`;
- таблицы: всё состояние (фильтры, сортировка, курсор, размер страницы) в URL, серверная
  курсорная пагинация, три разных пустых состояния (нет данных / фильтр пуст / ошибка),
  обычный `<table>` с `<caption>` и `aria-sort` на активной колонке;
- опасные действия по тяжести: обратимое — один клик и тост, среднее — alertdialog с
  фокусом на безопасной кнопке и обязательной причиной, которая уходит в журнал; ввод имени
  — только для необратимого удаления «контейнера»;
- права: deny-by-default, отдельные админские права, не пересекающиеся с правами обычного
  пользователя, проверка в каждом сервисе, решение по исходному пользователю;
- журнал: append-only, актор, цель, действие, причина, исход (включая отказы),
  correlation id, IP/UA; запись в той же транзакции, что изменение (outbox);
- имперсонацию («войти как») не делаем: вместо неё read-only карточка пользователя от
  имени самого админа, открытие карточки с личными данными пишется в журнал;
- блокировка обратима и в одной транзакции отзывает все refresh-семьи.

## Рекомендуемый подход

### Права
Сейчас роль `user` получает `manage` на `user`/`session`/`chat`/`game`/`calling`, так что
проверка «есть `game:moderate`» не отличит игрока от админа. Существующие права не трогаем,
админские живут в отдельных неймспейсах (миграция auth `000008_admin_console`):

| Неймспейс | Действия (корень `manage`) |
|---|---|
| `admin.users` | `read`, `mutate` (блокировка, отзыв сессий) |
| `admin.games` | `read`, `moderate` (прервать партию, отменить турнир) |
| `admin.chat` | `read`, `moderate` (удалить сообщение, исключить участника, завершить звонок) |
| `admin.audit` | `read` |
| `rbac` | `mutate` только у `admin`; `read` ещё у `moderator` |

Новая роль `moderator` (фиксированный UUID `...003`): `read` на все `admin.*` и `moderate`
на `admin.games`/`admin.chat`, плюс `rbac:read` (каталог ролей и фильтр пользователей по роли),
без `admin.users:mutate` и `rbac:mutate`. `admin` получает `manage`
на все `admin.*`.

Проверка в сервисах: общий пакет `core/shared/pkg/authz` — сервис спрашивает
`AuthAdminService/GetMyPermissions` с токеном самого вызывающего (mTLS-транспорт, без
общего ключа: `CALLS_INTERNAL_KEY` на базовом стенде не задан), кэширует набор прав на 30 с
по пользователю (ошибки не кэшируются), Connect-интерсептор держит карту «процедура →
право»; админская процедура без записи в карте отклоняется. Отказ — `PermissionDenied` и
событие `denied` в журнал. Tournament переходит с `GetMe` на `authz`. Тот же RPC отдаёт
права вебу, который прячет недоступные действия (сервер всё равно проверяет).

Защита от самоблокировки: нельзя заблокировать себя и снять `admin` с последнего админа —
проверка в той же транзакции под `SELECT ... FOR UPDATE` по `user_roles` роли `admin`.
`AssignRole`/`RevokeRole` сейчас пишут аудит отдельной вставкой после изменения и при ошибке
только растят счётчик — переводим их и все новые мутации на изменение + outbox в одной
транзакции.

### Журнал
- Событие `axon.events.v1.AdminAction` (`event_id`, `service`, `actor_id`, `action`,
  `target_type`, `target_id`, `reason`, `outcome` (`succeeded`/`denied`/`failed`),
  `correlation_id`, `client_ip`, `user_agent`, `details` map, `occurred_at`) — в топик
  `admin.audit.v1` из outbox сервиса, который сделал действие, в той же транзакции.
  Топик в группе `forever` коннектора, схема — в Apicurio.
- Read model в auth: консьюмер `auth` читает `admin.audit.v1` и `auth.audit.v1` в
  `auth.audit_log` (триггер запрещает `UPDATE`/`DELETE`, идемпотентность по `event_id`).
- Отступление: game и tournament пишут `AdminAction` отдельной вставкой в outbox сразу после
  изменения, не в одной транзакции — состояние партии живёт в Kafka-журнале, а отмена
  турнира — в транзакции саги. Остальные мутации (auth, chat, calls) — изменение + outbox в
  одной транзакции.
- `AuthAdminService/ListAuditEvents` — фильтры по актору, цели, сервису, действию, исходу,
  интервалу; курсор по `(occurred_at, event_id)`.

### API
Отдельный `admin.proto` на сервис, чтобы пользовательские контракты не разрастались:

- auth `axon.auth.v1.AuthAdminService` (префикс сервиса, чтобы `*AdminService` разных
  пакетов не путались в gateway, метриках и логах): `ListUsers` (поиск по email/имени/username, фильтр по
  роли и статусу, курсор), `GetUser` (профиль, роли, сессии, статус), `SuspendUser`,
  `RestoreUser`, `RevokeUserSessions` (все с `reason`), `ListAuditEvents`,
  `GetMyPermissions`. Блокировка — колонки `users.suspended_at`,
  `suspended_reason`; вход с верным паролем отвечает `PermissionDenied` «account is suspended» (до сброса
  троттлинга, так что статус узнаёт только владелец пароля), refresh — `Unauthenticated`,
  все refresh-семьи отзываются в той же транзакции, WS чата закрываются существующим
  `revocation.PublishUser`; access-токен живёт до своего TTL (15 мин) — game на отзыв не
  подписан, его WS отвалится при переподключении.
- game `axon.game.v1.GameAdminService`: `ListGameSessions` (все партии из проекции, фильтр
  по игре, статусу, игроку, турниру, курсор по `(updated_at, id)` — список «по последней
  активности»: партия, сделавшая ход во время листания, уходит наверх и видна после обновления;
  `created_at` в проекции нет, а при пересборке проекции он был бы временем пересборки),
  `CancelGameSession(session_id, reason)` через `Owner.Abort` с пересылкой владельцу партиции
  (как `forward.go`). Турнирные партии так не отменяются (`FailedPrecondition`: сага ждала бы
  результат вечно) — только вместе с турниром. `AbortSessionResponse.cancelled` (optional)
  говорит, что отмена реально случилась; если партия успела закончиться — `FailedPrecondition`
  без записи в журнал.
- tournament: `CancelTournament` и `StartTournament` для не-создателя — через
  `admin.games:moderate`, `reason` в запросе (новое optional поле, обязательно при override,
  в том числе с обычной страницы турнира), аудит; отказ не-создателю пишется как `denied`.
- rating `axon.rating.v1.RatingAdminService`: только чтение — лидерборд по игре с
  историей изменений игрока (`ListRatingChanges`).
- chat `axon.chat.v1.ChatAdminService`: `ListChatRooms` (все комнаты, тип, участники,
  последняя активность, курсор), `GetChatRoom` (участники), `ListChatRoomMessages` (только
  с `reason`, каждая страница — `room_messages_viewed` в журнал), `DeleteChatMessage` (цель
  в журнале — комната, id сообщения в `details`, чтобы история модерации собиралась по
  комнате), `RemoveChatMember` (так же: цель — комната, `user_id` в `details`). Звонки — отдельный `axon.calls.v1.CallsAdminService`
  (граница будущего выделения Calls, ADR-1): `ListAllCalls` (активные и недавние, фильтр по
  участнику, число активных), `TerminateCall` — `Mutate` с действием `timeout`, причина
  завершения `moderated`, `AdminAction` пишется в ту же транзакцию. Удаление и завершение
  идут теми же путями, что у участника (tombstone + рассылка в WS, события звонка).

Gateway: все новые процедуры `Public: false`; чтение `TierStandard`, мутации
`TierSensitive`.

### Web
- `/admin` — обзор: готовность сервисов, плитки (пользователи, заблокированные, живые
  партии, активные звонки, турниры в игре), последние события журнала со ссылками.
- Разделы (контекстный сайдбар «Administration»): Users, Games, Tournaments, Ratings,
  Chats, Calls, Audit log, Roles, Gateway, Service map. Пункты без права не показываются.
- Таблицы — `@tanstack/react-table` (новая зависимость) в `components/admin/data-table.tsx`:
  серверные сортировка/фильтры/курсор, состояние в URL (`useSearchParams` +
  `router.replace`), колонки с `aria-sort`, строка ведёт на детальную страницу, длинные
  списки — Virtualizer внутри `ScrollArea`.
- Детальные страницы: `/admin/users/[id]` (журнал действий над аккаунтом и самого
  аккаунта) и `/admin/chats/[id]` (история модерации); партии и турниры открываются по
  ссылке в существующих страницах игр — отдельные карточки не понадобились.
- Действия: `components/admin/confirm-action.tsx` — alertdialog с обязательной причиной,
  фокус на «Cancel», кнопка называет результат («Suspend user»), результат — `role="status"`.
- `roles-manager.tsx` удалён: роли выдаются на карточке пользователя (с причиной),
  `/admin/roles` — каталог ролей и их прав со ссылкой на участников; поиск по email — в
  таблице пользователей.
- Gateway и Service map доступны только роли `admin` (`RequireAdmin`).

### Этапы (отдельные PR)
Владелец выбрал все три этапа подряд в одной ветке.

1. **Фундамент и пользователи** — права и роль `moderator`, `GetMyPermissions` +
   `shared/pkg/authz`, `AdminAction` + журнал в auth, `AuthAdminService` (пользователи,
   блокировка, сессии, журнал), web: оболочка админки, обзор, Users, Audit log, Roles.
2. **Игры** — `GameAdminService`, tournament на `authz`, `RatingAdminService`, web: Games,
   Tournaments, Ratings, плитки обзора.
3. **Чаты и звонки** — `ChatAdminService` и `CallsAdminService`, web: Chats, Calls.

Не делаем: имперсонацию, массовые действия (на объёмах портфолио одиночных хватает,
`data-table` оставляет выбор строк выключенным), step-up повторный вход (нужен `auth_time` в
JWT — отдельная задача), встраивание Grafana.

## Известные ограничения
- Турниры в админке — `ListTournaments` без курсора: последние 100 на выбранный статус
  (`MaxListLimit`); курсор в публичный RPC — отдельная задача, турниров пока мало.
- `TestLoginAgainstAnOauthOnlyAccount` в auth флакает по таймингу (<1 мс на быстром хеше),
  к админке не относится.

## Файлы
- Proto: `core/shared/proto/axon/auth/v1/{admin,auth}.proto`,
  `axon/game/v1/admin.proto`, `axon/chat/v1/admin.proto`, `axon/calls/v1/admin.proto`,
  `axon/rating/v1/admin.proto`,
  `axon/tournament/v1/tournament.proto` (optional `reason`), `axon/events/v1/events.proto`
  (`AdminAction`); `make proto`.
- Общее: `core/shared/pkg/authz/` (клиент, кэш, интерсептор, запись отказов в outbox).
- Auth: `migrations/000008_admin_console.*`, `internal/domain/{rbac,admin}.go`,
  `internal/repository/{admin,audit_log}.go`, `internal/service/admin.go`,
  `internal/server/admin.go`, `internal/auditlog/` (консьюмер на franz-go: `shared/pkg/kafka`
  не принимает точки в имени события `admin.audit.v1`), `cmd/auth/main.go`.
- Game: `internal/server/admin.go`, `internal/session/admin.go` (запрос списка),
  `cmd/game/main.go`. Tournament: `internal/server/{server,roles}.go`.
  Rating: `internal/server/admin.go`, `internal/store/admin.go`. Chat: `internal/server/admin.go`,
  `internal/service/admin.go`, `internal/repository/{admin,calls_admin}.go`,
  `internal/calls/admin.go` (завершение модератором).
- Gateway: `internal/policy/policy.go`, `internal/proxy/admin.go`, `RATING_SERVICE_URL`.
- Deploy: `core/deploy/docker-compose.yml` (Kafka для auth, `AUTH_SERVICE_URL` у game/
  rating), `docker-compose.mtls.yml` (`MTLS_ALLOWED_CLIENTS` auth += rating, tournament),
  `cdc/outbox-connector.json` (`admin.audit.v1` в `forever` и в разрешённых топиках всех
  outbox), `kafka/topics.sh`, `docker-compose.{e2e,web-e2e}.yml`, `k8s/` — зеркально.
- Web: `ui/web/src/app/(app)/admin/**`, `src/components/admin/**`,
  `src/components/nav/{items,context-sidebar,command-palette}.tsx`, `src/lib/query/admin.ts`,
  `src/lib/admin/{access,permissions,url-state}.ts`, `e2e/admin.spec.ts` (роль выдаётся
  прямо в БД через `pg`, DSN — `ADMIN_POSTGRES_DSN` у контейнера playwright).

## Как проверяется
- `go test -race ./...` в auth, game, chat, tournament, rating и `shared/pkg/authz`:
  карта процедур (метод без права отклоняется), кэш решений, защита последнего админа,
  блокировка отзывает семьи и запрещает вход/refresh, аудит пишется в транзакции и при отказе.
- Integration (testcontainers): `auth.audit_log` неизменяем (UPDATE/DELETE падают),
  консьюмер идемпотентен по `event_id`; конкурентный revoke двух админов оставляет одного.
- Go e2e (`make test-e2e`): модератор листает пользователей, но `SuspendUser` —
  `PermissionDenied` и событие `denied`; админ блокирует → `Login` отказ → `RestoreUser` →
  вход работает; удаление сообщения модератором приходит участникам по WS, исключённый
  участник теряет доступ к группе (`auth/e2e/admin_test.go`, `chat/e2e/admin_test.go`).
- Integration: списки и курсоры `AdminRooms`/`AdminCalls`/`AdminList`/`Changes`,
  `TerminateCall` пишет аудит в транзакции и не пишет при отказе, модерация откатывается
  без строки аудита.
- Playwright (`make test-web-e2e`, `e2e/admin.spec.ts`): обычный пользователь не видит
  админку и получает отказ на прямой URL; админ находит пользователя в таблице, фильтр и
  сортировка переживают перезагрузку (URL), блокирует с причиной, событие в журнале;
  модератор видит разделы без кнопок блокировки; прерывание партии и удаление сообщения.
- `make lint` (golangci-lint, buf lint, eslint), `npm run typecheck`.
- Живьём: одноразовый проект `axon-admin` на других портах, сценарии в браузере, после
  ревью `docker compose -p axon-admin down`.

## Критерий готовности
Модератор и админ через `/admin` управляют пользователями, партиями, турнирами, чатами и
звонками; каждое действие проверено правом в сервисе-владельце и видно в журнале — это
подтверждают Go e2e и Playwright на Docker-стенде.
