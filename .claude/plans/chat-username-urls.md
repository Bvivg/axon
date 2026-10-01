# Username и адреса чатов (срез 7 переноса из nod)

Общий план: `chat-port-from-nod.md`. Запрос пользователя, в nod такого нет.

## Контекст

Чат открывается по `/chat/<uuid комнаты>`, новый личный — по `/chat/new/<uuid человека>`.
Такой адрес не продиктуешь и не узнаешь, с кем он. У людей нет уникального имени:
`nickname` необязателен, не уникален и только подменяет отображаемое имя.

Результат: у каждого аккаунта есть обязательный уникальный `username`. Личный чат
открывается по `/chat#bvivg` (username собеседника), группа — по `/chat#<uuid>`.
Старые адреса продолжают работать.

## Решения

### Username (auth)
- Колонка `users.username text NOT NULL`: только нижний регистр, `^[a-z][a-z0-9_]{2,31}$`
  (CHECK), уникальный индекс `users_username_key`. Ввод нормализуется: trim, ведущий `@`
  срезается, регистр понижается. Поэтому `#Bvivg` и `#bvivg` — один адрес.
  Дефиса нет, так что username и uuid не пересекаются, и адрес однозначно разбирается.
- Зарезервированные имена (`admin`, `axon`, `support`, `system`, …) нельзя выбрать.
- **Регистрация паролем** — поле Username в форме, обязательное
  (`RegisterRequest.username`, optional в proto ради совместимости; без него сервер
  сгенерирует имя, как для OAuth).
- **OAuth и старые аккаунты** — имя генерируется из локальной части email: `[^a-z0-9]+`
  превращается в `_`, ведущие не-буквы срезаются, длина ≤32; короче 3 символов —
  `user`. Если имя занято или зарезервировано — `<основа до 25>_<6 hex>`,
  до 5 попыток. Старые аккаунты получают такие же имена в миграции `000005_username`:
  дубликаты по `created_at` получают суффикс из id.
  Обязательного шага «выберите username» при входе нет. Колонка `NOT NULL`, и
  всем остальным частям системы не нужно обрабатывать человека без username.
  Сменить имя можно в профиле.
- Гонка: уникальный индекс решает, кто первым занял имя. На нарушении
  `users_username_key` выбор пользователя получает `AlreadyExists` («username is taken»),
  а генерация берёт следующий кандидат в новой транзакции.
- proto (additive): `User.username`, `PublicProfile.username`,
  `UpdateProfileRequest.username`, `RegisterRequest.username`; RPC
  `GetPublicProfileByUsername` (только с сессией, `TierStandard`) — gateway proxy + policy.
- Точный поиск людей (`SearchUsers`) находит и по username (`ada` и `@ada`).

### Адреса (web)
- Один маршрут `/chat`; чат выбирается хэшем. Канонический вид — `/chat#bvivg`. Next
  отвечает 308 с `/chat/` на `/chat`, хэш браузер сохраняет, поэтому `/chat/#bvivg`
  тоже работает.
- `lib/chat/address.ts`: разбор адреса из хэша, `chatHref()`, `useChatAddress()`.
  Адрес — внешний store над `location.hash`. Next меняет историю через `pushState` и
  событий не шлёт, поэтому store один раз оборачивает `history.pushState/replaceState`
  (как wouter) и слушает `hashchange`/`popstate`.
- Переходы между чатами идут не через `router.push`, а через `openChat`/`navigateTo`.
  Причина — баг Next 16.2: в кэше маршрутов `/chat` сохраняется `canonicalUrl` вместе с
  хэшем, взятым при первой загрузке или переходе. Следующие переходы склеивают его с
  новым хэшем. Получается устаревший хэш (`/chat` → `/chat#старый`) или двойной
  (`/chat#x#x`). В 16.3 починен только путь навигации, первая загрузка — нет.
  - Внутри `/chat` адрес меняется нативным `pushState/replaceState(null, …)`: Next
    принимает такую запись как restore и не трогает кэш.
  - В `/chat` с другой страницы переход идёт через роутер с запомненным намерением.
    Обёртка истории поправляет URL `replaceState`, как только Next запишет `/chat`.
  - Ссылки `<a href="/chat…">` перехватывает глобальный обработчик клика (capture).
    `hashchange` при ручной правке адреса синхронизирует состояние Next.
- Любое написание адреса (`#@BOB`, `#%20bob`, uuid в верхнем регистре) заменяется
  (`replace`) на канонический вид.
- `/chat`: адрес пуст — пустое состояние; uuid — комната; username — профиль через
  `GetPublicProfileByUsername`, затем личная комната с ним из списка комнат или
  новый личный чат. `ChatRoomView` получает `key` по цели, поэтому смена чата
  перемонтирует его, как раньше смена маршрута. Если адрес — uuid личной комнаты,
  он заменяется (`replace`) на `#username`.
  Неизвестный username — «User not found».
- `/chat/[roomId]` → `replace` на `/chat#<roomId>`; `/chat/new/[peerId]` удаляется
  (на него ссылались только диалог нового чата и палитра, теперь они ведут на `#username`).
- Активный чат во всех местах, где он раньше брался из `pathname` (layout, topbar,
  sidebar, tabbar, app-shell, тосты), берётся из `useChatAddress()`.
- Возврат после входа (`next=`) сохраняет хэш.
- Отображаемое имя без display name показывает username, а не обрезанный uuid.
- Push-уведомление ведёт на `/chat#<room uuid>` (у chat нет username собеседника,
  канонизацию делает клиент).
- Профиль: поле Username (с `@`) в Personal info. В меню аккаунта и профиле под
  именем показывается `@username`.

## Файлы
- `core/shared/proto/axon/auth/v1/auth.proto` + `make proto` (Go и TS gen).
- `core/services/auth/migrations/000005_username.{up,down}.sql`.
- `core/services/auth/internal/domain/{user,errors,validation,username}.go` (+ тесты).
- `core/services/auth/internal/repository/{user,search}.go`.
- `core/services/auth/internal/service/{password_flow,oauth_flow,profile,search,username}.go`,
  `store_fake_test.go` и тесты.
- `core/services/auth/internal/server/{handler,convert,errors}.go`.
- `core/services/auth/e2e/` — username: регистрация, занятость, поиск по username.
- `core/services/gateway/internal/{proxy/auth.go,policy/policy.go,policy/policy_test.go}`.
- `core/services/chat/internal/notify/notify.go` (+ тест) — URL уведомления.
- `ui/web/src/lib/chat/address.ts` (+ тест разбора), `app/(app)/chat/{page,layout}.tsx`,
  `app/(app)/chat/[roomId]/page.tsx`, удалить `app/(app)/chat/new/`.
- `ui/web/src/components/chat/{chat-address-view,use-room-directory,room-list-item,chat-activity,
  chat-room-view,new-chat-dialog}.tsx`, `components/nav/{items,topbar,app-sidebar,tabbar,
  app-shell,command-palette,account-menu}.tsx`, `lib/auth/guards.ts`, `lib/query/people.ts`,
  `components/auth/credentials-form.tsx`, `components/profile/personal-info.tsx`,
  `app/(app)/profile/page.tsx`, `lib/format.ts`.
- `ui/web/e2e/`: `support.ts` (username при регистрации), `chat-support.ts` (адреса,
  id комнаты из `data-room-id`), `chat`/`groups`/`notifications` под новые адреса, новый
  `usernames.spec.ts`.

## Проверка
- Unit: `go test ./...` для auth (валидация, генерация, регистрация/OAuth с коллизией,
  смена username, поиск), gateway policy, chat notify; vitest для разбора адреса.
- Integration: `make test-integration` — миграция на базе со старыми аккаунтами
  (дубликаты основ), гонка двух регистраций с одним username.
- E2E: `make test-e2e` (auth e2e username), `make test-web-e2e` — весь набор
  (адреса поменялись во всех чатовых спеках) + `usernames.spec.ts`:
  регистрация с username, занятый username в профиле, `/chat#<username>` открывает
  новый личный чат и после первого сообщения остаётся на том же адресе, старый
  `/chat/<uuid>` переходит на `#username`, группа — на `#<uuid>`.
- Живьём: дев-стек, `/chat#<username>` из адресной строки, назад/вперёд по истории
  между чатами, мобильная ширина (список ↔ чат по хэшу).

## Критерий готовности
Личный чат открывается по `/chat#<username>` собеседника, группа — по `/chat#<uuid>`,
username обязателен и уникален, все e2e зелёные.
