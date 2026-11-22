# Приложение = веб + нативные возможности

## Контекст
Владелец (2026-11-21): приложение должно повторять веб — та же стилистика, тот же функционал, в баре Игры, Звонки,
Чаты, Профиль; не урезать, а улучшить и дополнить нативными функциями. Первая версия редизайна
(`app-native-redesign.md`: вкладки Play/Games/Live/Profile, только игры) этому не соответствует — её оболочка
заменяется, а вход через GitHub по RFC 8252, Liquid Glass, доски и view models партий остаются.

Результат: SwiftUI-приложение (iOS + macOS) с четырьмя разделами веба, каждый с функционалом веба, в палитре и
типографике веба, плюс нативные функции, которых у веба нет.

## Что есть на вебе (опись 2026-11-21)
- **Games**: каталог из 5 игр (Tic-Tac-Toe, Chess, Quoridor, Checkers, Durak) + Tournaments; лобби игры — рейтинг,
  Start a game (Quick match, Private game по ссылке, Practice offline против бота Easy/Medium/Hard или вдвоём),
  Live games, Your games; партия, наблюдение, реплей, анализ шахмат; Дурак — столы 2–6, режимы, шулеры, боты;
  турниры — список, создание (Bracket/Swiss), регистрация, старт/отмена, сетка, standings.
- **Calls**: история с фильтрами и поиском, New call, входящие (Accept/Decline/Answer with audio), 1:1 и группы,
  invite/remove, mute, камера, устройства, screen share, свернуть в мини-бар, PiP, миграция транспорта.
- **Chats**: комнаты с поиском, люди, DM, группы (создание, переименование, участники, Leave, Hide); текст, голосовые,
  фото, видео, файлы, альбомы, подписи, reply, forward, edit, delete (в т.ч. пачкой), выделение; typing, presence,
  прочтения и «кто прочитал», поиск по сообщениям, медиа и файлы комнаты, просмотрщик медиа; Contacts, Media.
- **Profile**: My profile, Personal info (аватар, имя, username, email + статус), Sessions (Revoke),
  Sign-in methods (привязка провайдеров), Notifications, Appearance (Light/Dark/System).
- API: Auth/Chat/Calls/Game/Tournament/Analysis Connect; WS `/ws/chat` (чат + сигналинг звонков `call_*`),
  `/ws/presence`, `/ws/game`; загрузки — REST `/api/uploads` (сессии, части, докачка).

## Стиль (как на вебе)
- Палитра из `ui/web/src/app/globals.css`: фон `#f3f4f8`/`#0e121b`, primary `#5945b9`/`#ad9ffc`, success/warning/
  destructive, отдельные оттенки входящих/исходящих сообщений. Карточки — поверхность + 1px граница, радиус 14–16.
- Шрифт Geist / Geist Mono (OFL) встроен в приложение, плотный трекинг как на вебе; eyebrow-подписи секций.
- Иконки игр — линейные знаки в сиреневых плашках (как в каталоге веба), не цветные градиенты.
- Навигация: на iPhone — системный стеклянный таб-бар Games/Calls/Chats/Profile (повторяет плавающий бар
  мобильного веба); на iPad/macOS — `.sidebarAdaptable` = рейл + список раздела + содержимое, как три колонки веба.

## Нативное поверх веба
- iOS: системный Liquid Glass, haptics, контекстные меню и свайпы (ответить, переслать, удалить), PhotosPicker и
  камера, запись голосовых через AVAudioRecorder, Quick Look для файлов, Share Sheet и приём «Поделиться» в чат,
  drag & drop, Dynamic Type, VoiceOver.
- Звонки: CallKit (системный экран звонка, история в «Телефоне»), AVAudioSession с выбором выхода, PiP видео.
- Игры: Live Activity «ваш ход» / идущий звонок, виджет «мои партии», App Intents (Quick match в Shortcuts/Siri),
  Spotlight по партиям и чатам.
- macOS: меню и клавиши (Cmd-K палитра как на вебе, Cmd-N новый чат), несколько окон.
- Локальные уведомления о сообщениях/ходах, пока приложение живо; фоновые push — см. зависимости.

## Решения владельца (2026-11-21)
- Apple — только бесплатный Personal Team: APNs и PushKit не делаем; уведомления локальные, пока приложение живо;
  CallKit — для звонков, пока приложение запущено.
- Пакеты звонков — оба: stasel/WebRTC (1:1 через coturn) и LiveKit Swift SDK (группы), как на вебе.
- Порядок: оболочка → чаты → звонки → игры → нативные дополнения.

## Этапы (каждый — ветка и PR)
1. **Оболочка, стиль, профиль** — `feat/app-native-redesign` (уже содержит вход через GitHub). Вкладки
   Games/Calls/Chats/Profile, токены веба, Geist; Games — каталог + лобби игры (рейтинг, Start a game, Live, Your
   games) на существующих досках; Profile — все пять пунктов + Sign-in methods (привязка из приложения:
   снять запрет `the app cannot link providers`, link-поток с app_challenge) + адрес gateway + выход.
   Calls/Chats — пустые экраны раздела со списком из API (история звонков, список комнат) до этапов 2–3.
2. **Чаты** — `feat/app-chat`: `ChatAPI` (Connect), `ChatSocket` (`/ws/chat`, протокол из `lib/ws/protocol.ts`),
   загрузчик `/api/uploads` с докачкой; экраны комнат, переписки (ленивая лента, подгрузка истории сверху),
   групп, людей, поиска, медиа; все типы сообщений веба.
3. **Звонки** — `feat/app-calls`: `CallsAPI`, сигналинг `call_*` по `/ws/chat`, WebRTC 1:1 через coturn
   (`GetMediaCredentials`), группы через LiveKit (`GetSfuCredentials`) — как на вебе; история, входящие, экран звонка,
   CallKit, PiP. Screen share с iPhone — broadcast extension, отдельным шагом.
4. **Игры целиком** — `feat/app-games-parity`: Дурак (стол 2–6, режимы, Catch, перетаскивание, комнаты, боты),
   Practice offline (боты на Swift по логике `lib/games/*-bot.ts`, игра вдвоём), турниры, анализ шахмат в реплее,
   наблюдение.
5. **Нативные дополнения** — `feat/app-native-extras`: Live Activity, виджет, App Intents, Spotlight, macOS-меню,
   Share Extension.

## Затрагиваемые файлы (паттерн)
- `ui/app/Features/<Section>/` — экраны раздела (`Games/`, `Calls/`, `Chats/`, `Profile/`), view models рядом.
- `ui/app/Core/Networking/` — `ChatAPI`, `CallsAPI`, `TournamentAPI`, `ChatSocket`, `Uploader` по образцу `GameAPI` /
  `GameChannel`; фейки в `Tests/AxonTests/Fakes/`.
- `ui/app/Shared/UI/` — `Theme` (токены веба), шрифты Geist в `App/Fonts`, `project.yml` (ресурсы, пакеты).
- Бэкенд — только где веб уже не покрывает приложение: auth (привязка провайдера из приложения), chat (APNs —
  если будет платный аккаунт).

## Как проверяется (на каждом этапе)
- `make app-lint`, `xcodebuild test -scheme Axon` (unit), сборка macOS, `make test-e2e-app` (XCUITest на
  одноразовом стенде) — новые UI-тесты на каждый раздел: чат между двумя аккаунтами, звонок (оркестрация, медиа
  можно мокать), профиль (имя, сессии), Дурак/турнир.
- Симулятор iPhone 18 Pro против стенда `axon`: скриншоты раздела в светлой и тёмной теме рядом со скриншотом веба
  той же страницы (`.playwright-mcp/web-*-mobile.png`), живой сценарий против второго участника в вебе.

## Критерий готовности
В приложении все четыре раздела веба с тем же функционалом и в той же стилистике, плюс перечисленные нативные
функции; тесты зелёные, сценарии каждого раздела пройдены в симуляторе против стенда.

## Этап 1 — что сделано и расхождения (2026-11-21)
- Оболочка: `AppTab` = games/calls/chats/profile, `Route` = lobby/game/replay/profile(page); по стеку навигации на
  вкладку; бейдж непрочитанного на Chats.
- Стиль: `Shared/UI/Theme.swift` — токены веба (light/dark через UIColor/NSColor provider), Geist/Geist Mono,
  `Eyebrow`, `IconTile`, `surface` (карточка с границей 1px, радиус 16), `EmptyState`. Шрифты регистрируются через
  CoreText в `Theme.configureAppearance()`: `UIAppFonts` регистрирует их после `App.init`, и заголовки навбара
  оставались системными. Заголовки навбара — через `UINavigationBarAppearance` (legacy-атрибуты iOS 26 игнорирует).
- Games: каталог (рейтинг из `GetRatings`, поиск, «In progress»), лобби игры (рейтинг, Start a game → Quick match /
  Private game / Join with a link, Live games, Your games). Durak, Tournaments и Practice offline — этап 4, в каталоге
  пока не показываются.
- Chats/Calls: списки из `ListRooms`/`ListCalls` с поиском (у звонков ещё фильтр как на вебе); открытие комнаты,
  New chat и New call — этапы 2–3.
- Profile: карточка, My profile (email + статус, Member since, роли, выход), Personal info (`UpdateProfile`),
  Sessions (Sign-in methods + сессии, End session), Notifications (звуки, локальные уведомления), Appearance
  (Light/Dark/System), Gateway. Смена аватара — этап 2 (нужен загрузчик `/api/uploads`).
- Бэкенд: привязка провайдера из приложения — `StartAppOAuth(link)` с проверкой владельца при завершении, как у веба.
- Приложение шлёт `User-Agent: Axon/<версия> (iOS|macOS x.y)` — в Sessions видно «Axon app on iOS».
- Стенд: gateway пришлось пересобрать — старый образ с прежним proto отбрасывал `app_challenge`, и вход/привязка из
  приложения уходили на веб-callback вместо `/auth/callback/<provider>/app`.

## Этап 2 — что сделано и расхождения (2026-11-22)
- Сеть: `ChatAPI` на все RPC чата (`LiveChatAPI`), `searchUsers` в auth; `ChatSocket` (`/ws/chat`, протокол
  `axon.chat.v1`, переподключение с backoff, обновление токена, 4403 → выход); `ChatWire` — все кадры веба
  (`lib/ws/protocol.ts`), payload → proto-типы; `MediaAPI` — загрузка `/api/uploads` с докачкой частей (статус →
  только недостающие) и медиа `/api/media` с Bearer только для origin шлюза.
- Хаб `ChatsViewModel`: один сокет на сессию, подписки на все комнаты с `since`, presence, typing, непрочитанные,
  превью; профили всех упомянутых людей (авторы, участники системных сообщений) грузятся до показа имени.
- Переписка: ленивая лента с подгрузкой истории сверху, якорь первого непрочитанного, кнопка «вниз»; все типы веба —
  текст, голос, фото, видео, файл, альбом, подпись, ответ, пересылка (с комментарием), правка, удаление (в т.ч.
  нескольких), выбор, typing, presence, прочтения и «Seen by»; исходящие с ack/эхом/таймаутом/повтором.
- Экраны: список комнат + люди в поиске, New message / New group, инфо контакта/группы (переименование,
  добавить/исключить, выйти/удалить), медиатека Media/Files/Voice, поиск по чату с переходом и подсветкой,
  просмотр медиа, файлы через QuickLook.
- Нативное: локальные уведомления о сообщениях из других чатов (баннер, тап открывает чат). Только пока приложение
  живо: при уходе в фон сокет закрывается, а APNs на Personal Team нет — уведомления в фоне вне объёма.
- Видимость как в вебе: `sceneVisible` (не `.background`) глушит баннер и счётчик открытого чата, `sceneActive`
  (`.active`) нужен для отметки прочтения. При возврате в `.active` сокет переподключается (`persistent` policy),
  список комнат и открытая лента догружаются (`refresh(with:)` применяет и правки, сделанные пока были в фоне),
  баннер на одно сообщение не повторяется (dedupe по id).
- Прокрутка ленты: `ScrollPosition.scrollTo(id:)` запоминает цель и пересчитывает её на каждом relayout ленивой
  ленты с оценочными высотами — лента прыгала. `scrollTo(edge:)` в скрытой ленте добавлял сдвиг 12pt по x. Итог:
  разовый `ScrollViewReader.scrollTo` последней строки; первая прокрутка после загрузки без анимации; признак «внизу»
  меняют только пользовательские фазы прокрутки; доводка вниз — после окончания программной анимации.
- Accessibility: идентификатор на составном `ConversationView` перекрывал детей — нужен
  `.accessibilityElement(children: .contain)`.
- Сборки для симулятора — с подписью: без неё Keychain не работает и сессия теряется при перезапуске.
- Голосовые сообщения не проверены живьём: симулятору нужен доступ к микрофону macOS (системная настройка).
- XCUITest `ChatFlowTests`: регистрация, поиск собеседника, отправка (доставка подтверждена через `ListRooms`
  собеседника), ответ собеседника по `/ws/chat`, превью в списке комнат.
