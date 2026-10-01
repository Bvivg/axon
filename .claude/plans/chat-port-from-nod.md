# Чат: перенос доработок из nod обратно в axon

## Контекст

28.09 чат axon (состояние ≈ `efd8e00` + WIP групп) перенесён в `~/Developer/SSI/nod`
(NestJS + socket.io + Vite/FSD/radix) и там доведён: лента без скачков, выбор сообщений,
медиа v2 (чанки, альбомы, дедуп), уведомления с Web Push, поиск. Модель в nod другая —
тред между парой кабинетов + задачи; в axon — классические direct/group.

Переносим **поведение и найденные решения, не код 1:1**: бэкенд переписывается на Go
(proto + Connect + свой WS-протокол + gateway), фронт адаптируется под Next.js + Base UI +
дизайн Signal. Результат: чат axon по UX и надёжности равен nod, без кабинетной специфики.

**Не переносим:** кабинеты/пары, задачи и карточки задач, системные `task_status`,
Mini App, Telegram и утренний дайджест, колокольчик-центр уведомлений (в nod он общий
для всего кабинета), локализацию ky (UI axon английский).

## Предусловия

1. `/ship` текущего WIP (`feat/chat-groups-and-message-actions`) → PR #9 → мерж в main
   (решение пользователя). Дальше каждый срез — от свежего main.
2. Источник — nod `main` (`75706f0`). В nod не закоммичены swipe-to-reply, правки меню
   сообщения и композера — их переносить после коммита в nod (срез 2), иначе зафиксировать
   дату снятия в плане среза.
3. Каждый срез: своя сессия, свой детальный план `.claude/plans/chat-<scope>.md` до кода,
   своя ветка `feat/chat-<scope>`, свой PR. PROGRESS.md — на время среза.

## Срезы (порядок = зависимости)

### 1. Лента и поле ввода — `feat/chat-transcript` (только web)
- `virtua` `Virtualizer` вместо `@tanstack/react-virtual` в ленте: `shift` при подгрузке
  прошлого (в nod замер: TanStack сдвигал экран на 32 px и терял кадр), `overflow-anchor:
  none`, распорка `flex-grow` прижимает короткий тред к низу, первый показ скрыт до устойчивой
  высоты, `ResizeObserver` держит низ; пороги 400 px (подгрузка) / 80 px («у низа»).
- Разделитель «Unread messages» по `last_read_seq`, открытие на первом непрочитанном,
  кнопка «вниз» со счётчиком новых.
- Переход к сообщению через ≤20 страниц истории, центровка после устойчивой высоты,
  подсветка 1600 мс (цитата ответа; позже — результаты поиска).
- Серии: имя/аватар один раз на серию (один автор, разрыв ≤5 мин, ≤30 сообщений).
- Ссылки — `linkifyjs` (http/https/mailto, без хвостовой пунктуации, `noopener noreferrer`).
- `MarkRead` с debounce 250 мс и только при видимой вкладке.
- Поле ввода многострочное с автоувеличением (shadcn `input-group`), Enter — отправить,
  Shift+Enter — перенос; drop файлов на ленту.
- Сообщение сокета, пришедшее во время первой загрузки истории, дописывается через
  `QueryCache.subscribe` (TanStack не отменяет первую загрузку без данных).
- `vitest` в `ui/web` для чистой логики (`make web-test`, шаг в CI) + перенос тестов nod.
- `rules/nextjs-web.md`: лента чата — `virtua` (одна строка «почему»), прочие списки —
  по-прежнему TanStack Virtualizer.

Файлы axon: `ui/web/src/components/chat/{transcript,message-content,message-composer,chat-room-view}.tsx`,
новые `ui/web/src/lib/chat/{series,links,navigation}.ts`, `ui/web/package.json`, `Makefile`,
`.github/workflows/*`, `.claude/rules/nextjs-web.md`.
Источник nod: `frontend/src/widgets/chat-thread/ui/transcript.tsx`, `widgets/chat-thread/lib/series.ts`,
`entities/chat/lib/{split-links,navigation}.ts`, `entities/chat/model/cache.ts`,
`features/send-message/ui/message-composer.tsx`, `.claude/research/chat-transcript-virtualization.md`.

### 2. Выбор сообщений, пакетные действия, «кто прочитал» — `feat/chat-selection`
- Бэк: миграция `000006` — `chat.read_marks (room_id, user_id, from_seq, to_seq, read_at)`,
  индекс `(room_id, to_seq)`; `MarkRead` пишет диапазон в той же транзакции, что и курсор.
- proto (additive): `ListMessageReaders(message_id)` → `[{user_id, read_at}]` (только
  участник); `ForwardMessages(message_ids ≤100, targets ≤50, comment)` — транзакция на целевую
  комнату, порядок сохраняется, файлы не копируются; WS `delete` получает `message_ids[]` (≤100).
  Пересылку пересланного оставляем как в axon (с исходным автором) — nod её запрещает,
  но у axon подделка автора исключена.
- gateway: policy + proxy для новых RPC.
- Веб: выбор кликом и протягиванием (автоскролл у края 48 px), нельзя выбрать системные и
  удалённые; панель выбора Forward / Copy / Delete со счётчиком; пересылка в несколько
  чатов с комментарием; в меню сообщения — «Read by» со временем; swipe-to-reply на таче
  (порог 56 px, максимум 72 px); long-press открывает меню; копирование картинки.
- Источник nod: `widgets/chat-thread/lib/{selection,drag-select,swipe-to-reply}.ts`,
  `ui/{selection-bar,message-actions-menu,message-bubble}.tsx`,
  `features/{forward-message,message-readers}`, `entities/chat/lib/copy-image.ts`,
  `backend/src/chat/chat.service.ts` (forward, readers).

### 3. Медиа v2: загрузка и обработка — `feat/chat-media-pipeline`
- Загрузка частями через gateway (HTTP, как нынешний upload): start → `PUT parts/{n}`
  по 10 МБ → complete (истина — `ListParts` MinIO, не счётчик в памяти) → `DELETE`;
  лимит 200 МБ из env; докачка уже загруженных частей.
- Статусы `uploading → processing → ready | failed`, очередь обработки в один воркер,
  WS-событие `attachment_updated` автору.
- Дедуп по содержимому: `chat.files (kind, sha256)` + `source_sha256` у загрузки — тот же
  файл повторно не обрабатывается; GC файлов по `linked_at` с запасом 1 ч.
- Картинки → WebP ≤2560 (lossless, если выигрыш <50%), EXIF-поворот, без метаданных;
  видео H.264 CRF25 ≤1080p ≤30fps, AAC 128k, `+faststart`, совместимые — только remux;
  документы — brotli при выигрыше ≥10% (`Content-Encoding: br`); Range/If-Range;
  SVG/HTML — `attachment` + `nosniff`.
- Черновики: удаление при снятии вложения, уходе из чата и закрытии вкладки
  (`pagehide` + `fetch keepalive`); hold/release на время отправки (гонка «убрал во время
  complete» — тест, падающий без фикса). Уборщик axon дорабатывается, не переписывается.
- Веб: части, прогресс, отмена, ретрай (`use-pending-uploads`), HEIC → `heic2any` на клиенте.
- Миграция `000007`: `files`, статусы/части у загрузок, перенос существующих `uploads`.
- axon уже имеет `core/services/chat/internal/media/{image,ffmpeg}.go`, `attachment/sweeper.go` —
  расширить их. Источник nod: `backend/src/chat/{chat-upload,attachment-processing,chat-files,chat-upload-sweeper}.service.ts`,
  `backend/src/chat/media/*`, `.claude/research/chat-media-storage.md`, `entities/chat/lib/{upload,use-pending-uploads}.ts`.

### 4. Альбомы, просмотрщик, плеер — `feat/chat-albums`
- До 10 вложений в сообщении: `chat.message_attachments (message_id, attachment_id, position)`;
  proto `Message.attachments` (repeated, additive; старые сообщения читаются из `payload`),
  отправка `upload_ids[]`, пересылка части альбома.
- Сетка альбома (плитка 140–280 × ≤320), подпись, превью в списке «Album · N».
- Лайтбокс `yet-another-react-lightbox`: галерея по медиа комнаты, zoom ×4, свайп,
  клавиши, скачать; играет только активное видео.
- Плеер `media-chrome` + `@player.style/minimal` (компактная раскладка <384 px).
- Инфо чата: вкладки Media / Files / Voice (расширить `room-media.tsx`).
- Источник nod: `entities/chat/ui/{message-media,media-lightbox,video-player}.tsx`,
  `widgets/chat-room-info/ui/room-info-sheet.tsx`.

### 5. Уведомления — `feat/chat-notifications`
- WS-кадры клиента `visibility {visible}` и `focus {room_id|null}`; presence (Redis) хранит
  их по сессии.
- Решает сервер: получатель смотрит эту комнату → ничего; вкладка видима → тост
  (WS `notify`); скрыта или офлайн → Web Push. Отправка best-effort после коммита,
  не блокирует `send`.
- Web Push: VAPID-ключи (`make vapid-key` → `core/deploy/.env`), `chat.push_subscriptions`,
  RPC `SubscribePush` / `UnsubscribePush`, `public/sw.js` (userVisibleOnly, клик открывает
  чат), Go-библиотека — выбрать в плане среза с проверкой версии.
- Веб: тосты, счётчик непрочитанных в заголовке вкладки, включение уведомлений в профиле.
- Источник nod: `backend/src/chat/chat-notify.service.ts`, `backend/src/realtime/*`,
  `frontend/src/features/{enable-web-push,notify-manager}`, `frontend/public/sw.js`.

### 6. Поиск по чату — `feat/chat-search`
- `pg_trgm`: `CREATE EXTENSION` требует суперпользователя → в init-скрипт Postgres
  (`core/deploy`), не в миграцию сервиса; GIN-индекс по `messages.body` — в миграции `chat`.
- RPC `SearchMessages(room_id, query 2..100)` → ≤50, только участник, без удалённых.
- Панель поиска в шапке чата (debounce 300 мс), клик → переход к сообщению из среза 1.
- Источник nod: `widgets/chat-thread/ui/search-panel.tsx`, `chat.service.ts` (search).

### 7. Username и адреса чатов — `feat/chat-usernames` (запрос пользователя, не из nod)
- auth: обязательный уникальный `username` (латиница, цифры, `_`, 3–32, без учёта
  регистра — уникальный индекс по `lower(username)`), задаётся при регистрации; у
  старых аккаунтов и после OAuth — обязательный шаг при входе, без него дальше не
  пускаем. Смена — в профиле. Зарезервированные слова (`new`, `admin`, …) запрещены.
- RPC поиска человека по username (через gateway), username — в `User` и публичном
  профиле (additive).
- Адреса: страница `/chat`, чат выбирается хэшем — `/chat/#bvivg` (личный, по
  username собеседника), `/chat/#<uuid>` (группа). Старые `/chat/<uuid>` и
  `/chat/new/<peerId>` перенаправляют на новые.
- Проверка: unit/integration (уникальность без учёта регистра, гонка двух
  регистраций), e2e регистрации с username, переход по `/chat/#username`, обновление
  всех web e2e под новые адреса.

## Сквозное для каждого среза
- proto только additive + `make proto`; миграции up/down; новые RPC — в gateway policy/proxy;
  код без комментариев; тексты UI английские.
- DoD: `make check`, `make lint`, `make test-integration`, `make test-e2e`, `make web-check`,
  `make web-test`, `make test-web-e2e` (стек перед e2e остановить) — зелёные; живьём во
  встроенном браузере (десктоп + 375 px, светлая/тёмная); `/code-review` до пустого раунда.
- Новый e2e на каждый пользовательский флоу среза (`rules/testing.md`): `ui/web/e2e/*.spec.ts`
  + `core/services/chat/e2e/*_test.go`.

## Проверка ключевых обещаний
- Срез 1: тред 3000 сообщений (сид через WS) — подгрузка прошлого 0 px сдвига
  (`javascript_tool`: позиция якорного сообщения до/после), в DOM ≤30 строк, переход к
  старому сообщению — по центру ±2 px; e2e: открытие на непрочитанном, «вниз», Shift+Enter.
- Срез 2: e2e — выбрать 3 → переслать в 2 чата с комментарием; «Read by» в группе.
- Срез 3: e2e — 150 МБ видео частями, обрыв и докачка; повторная загрузка того же файла
  не создаёт новый объект в MinIO; закрытие вкладки удаляет черновик.
- Срез 4: e2e — альбом из 4 фото, лайтбокс листается клавишами.
- Срез 5: вкладка скрыта → push (проверка в Chrome пользователя, разрешение даёт он);
  комната открыта → ни тоста, ни push.
- Срез 6: e2e — поиск слова → переход к сообщению на 10+ страниц назад.

## Что понадобится от пользователя
- `/ship` WIP перед срезом 1.
- Срез 5: разрешение уведомлений в Chrome для живой проверки push.
- Срез 3: образец HEIC (или ок на синтетический).

## Критерий готовности
Все семь срезов смержены в main, и чат axon ведёт себя как nod по списку выше без
кабинетной специфики.
