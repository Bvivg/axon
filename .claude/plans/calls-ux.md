# Calls UX — макеты и четыре волны

Ревизия 2026-11-12. Владелец принял v3 как визуальный референс, согласовал диагностику, первую правку 1A и переход к звонку без чата в 1B. Изменение контракта 1B additive. Миграции, флаги и commit/push/PR вне объёма.

Аудит старого UI и проверки v2 лежат в `.local/evidence/calls/UX-audit-2026-11-10/` и `.local/evidence/calls/UX-review-v2-2026-11-10/`. Путь и контрольная сумма принятого v3 записаны в `.local/evidence/calls/UX-review-v3-2026-11-12/README.md`. V3 не проверялся на реальных устройствах. Камера в нём иллюстративная, fullscreen и PiP — заглушки. Результаты проверок v2 относятся только к v2.

## Решения, общие для волн

- Calls остаётся внутри Chat;1:1 P2P+coturn, группы SFU, существующий R3 handoff сохраняется. Новый UI/state framework/отдельный calling service не нужен. Единственный CallManager/media owner и единственный audio renderer на signed-in shell.
- Новый direct call не требует и **не создаёт комнату**: `room_id=NULL`. Calls history независима от переписки. Declined/missed/cancelled/failed/completed без сообщения дают0новых чатов. Первый настоящий SendDirect создаёт/переиспользует комнату; Call.room_id задним числом не переписывается. Open chat открывает виртуальную переписку, сам просмотр её не создаёт.
- Auth/family/selected endpoint/epoch/generation/media_version/lease/consent/busy/outbox сохраняются. Отсутствие room_id не означает отсутствие ACL. Старый StartCall(room_id), group-chat membership и исторические вызовы совместимы.
- Только Docker для server/build/automated tests. Реальные браузеры/устройства — клиенты Docker stand. Без mock RTCPeerConnection/LiveKit Room; существующие E2E30s/expect10s/retries0 и production limits не ослаблять. Новые UI-строки английские, dark/light — текущие токены AXON.
- Каждая волна закрывается отдельно. Автоматический media gate сохраняет selected ICE direct/relay, RTP/audio energy/video deltas, actual room/participants, сохранённые track refs=ended/PC=closed и slots0 после End. Для изменений только представления дополнительно доказать неизменность capture/PC/Room/Call ID/StartedAt и отсутствие второго output. Макет/скриншот не является media evidence.

## Макеты до app-кода

- Один набор компонентов для P2P и SFU на всех экранах. В 1:1 нет remote mute и индикатора качества `Good connection`. Отсутствие видео собеседника в P2P подписывается `No video`. Бейджи `Muted` и `Speaking` доступны только в SFU-группе по достоверному состоянию из SDK.
- Подписи `Microphone` и `Camera` называют управление. Иконка, пометка `Off` и `aria-pressed` показывают состояние. `aria-label` описывает действие для этого же управления (`Mute microphone`, `Turn camera on`), без расхождения с видимой подписью.
- PiP входит в волну 4. Кнопка появляется при наличии реального video track и поддержке API. `Keep screen on` находится только в Devices/settings.

| Экран | Предлагаемое поведение | Волна |
|---|---|---|
| Большая сцена | Утверждённая основа: большая remote tile, self-preview, устойчивая панель | 2 |
| Компактная в чате | Узкая in-flow плашка: name/time/mic/End/Return; на телефоне2строки, над composer; никакого Video call message в transcript | 2 |
| Мобильный ландшафт 844×390 | Self-preview внутри основной плитки. End в углу шапки, отдельно от колонки mic/camera/devices/people. Статус в шапке, Keep screen on в Devices/settings | 2–3 |
| Портрет390×844 | Основной1:1: thumb-reachable controls, safe-area, полная сцена | 2–3 |
| Incoming audio/video/group | Имя звонящего/название группы, большие Accept/Decline, video→Answer with audio | 3 |
| Camera on | Видео занимает основную площадь, подпись не заслоняет controls | 2 |
| Group3/6 | Сетка и аватары. Muted/Speaking только в SFU-группе и только по данным SDK | 2–3; Speaking — 4 |
| Screen share | Экран в фокусе, участники миниатюрами; существующий share сохраняется | 2–3 |
| Permission denied/pending | Понятный текст/Try again/Cancel; pending остаётся видимым и отменяемым | 3 |
| Reconnecting | Shell и End остаются; понятное восстановление, без нового Call ID | 2–3 |
| New call | Поиск, выбор человека, Audio/Video без сообщения | 1 |
| History | Call back, Missed как исход, короткие даты; без обещания server missed-filter | 3 |

Все экраны показать в dark/light; длинные имена переносятся и доступны полностью. Touch targets≥44×44, размеры320×568/390×844/844×390/1440×900. Допустима вертикальная прокрутка длинных списков/participants/settings при неподвижных основных controls; горизонтального scroll и скрытых подписью действий быть не должно. Одни компоненты для P2P и SFU. Включённая камера в макете — нарисованное иллюстративное превью, не реальный человек/захват камеры.

## Волна 1 —429, затем звонок без чата

### Входит

**1A — первый блокер, 429 на media RPC.** Наблюдение от 2026-11-12 завершено. В штатном 3-party SPA при WS attach replay старых версий вызвал `apply → historyChanged → invalidateQueries → ListCalls`, повторные запросы профилей и 429 на чтениях. Три media RPC в штатном звонке прошли. В ускоренном harness с reload та же standard-квота пользователя исчерпалась во время подключения и End. `GetSfuCredentials`, `ReportConnection` и `AcknowledgeCleanup` получили 429 от gateway и прошли после transport retry. Sensitive-отказы refresh и register относятся к другим buckets. Точный баланс gateway не экспортируется, в waterfall есть границы и оценки. См. `.local/evidence/calls/1A-429-diagnosis-2026-11-12/README.md`.

Первая правка 1A выполнена. Coalesce 600 мс дожидается текущего чтения и сохраняет следующий refresh при изменениях в полёте. `ListCalls` в SPA сократился 131→16, 429 браузера 7→0. В полном повторе reload `ListCalls` 290→13. Остались 429 на холодных чтениях и отдельных media RPC: в одном ускоренном прогоне Cleanup, в другом Credentials. Поэтому устойчивость к reload остаётся открытой. Доказательства и варианты следующего шага в `.local/evidence/calls/1A-429-fix-2026-11-12/README.md`. Лимиты 120 standard/min и 10 sensitive/min сохранены. Разделение read/control quota требует отдельного обоснования.

**1B — roomless direct.** Additive typed `StartDirectCall(peer_user_id,mode,endpoint,request_id)`. Проверить actor/family/endpoint, существование доступного peer через identity boundary, запрет self. Одной transaction создать Call/participants/slots/request receipt/outbox, `room_id=NULL`; room/room_members не создавать. Fingerprint включает target+mode+endpoint; replay с другим target запрещён. До принятого Start permission denial/Cancel не создаёт Call, после него неудачные исходы остаются в истории.

Статус 1B: входы из пустой переписки, поиска и Calls → New call работают на локальном стенде. Подтверждены direct и relay в Docker, запись у обоих участников, отсутствие чата до первого сообщения и прежний ACL при Add person. Relay проверен на временном coturn; LAN TURN на устройствах остаётся непроверенным. Результаты и SQL в `.local/evidence/calls/1B-roomless-direct-2026-11-12/README.md`.

GetOrCreateDirectRoom остаётся только в SendDirect. Не вызывать helper из Calls.Mutate: он сам делает InTx, transactional Repository запрещает nested transaction. Concurrent first SendDirect использует текущий rooms_direct_pair_key; новый вызов вообще не зависит от создания этой пары. No fake message/side-effecting GET/mass backfill historical room links.

Frontend: `New call` на Calls, picker через текущие SearchUsers/DirectoryList, кнопки в виртуальной пустой переписке и поиске. PersonRow с call actions разделить на sibling interactive controls, без button внутри button. Existing room/draft не терять. Presence не считать гарантией дозвона или запретом звонить offline.

### Не входит

Новый shell, fullscreen/PiP, WakeLock, sounds, shortcuts, server missed-filter, remote media-state RPC. Новый список контактов/blocklist/privacy settings не добавлять; текущие auth/rate/consent сохраняются. Не расширять group invitation ACL скрытно под видом roomless direct.

### Файлы

- Диагностика: `ui/web/src/lib/calls/manager.ts`, `components/calls/call-provider.tsx`, `lib/query/calls.ts`, `lib/query/people.ts`, `lib/calls/channel.ts`, auth refresh/connect retry; `core/services/gateway/internal/guard`, `policy`, `ratelimit` — только по доказанной причине.
- Контракт: `core/shared/proto/axon/calls/v1/calls.proto`, generated Go/TS; Chat calls domain/service/proto/handler, `repository/calls.go`, `call_requests.go`, identity/push adapters; gateway Calls proxy/policy.
- Nullable direct: **`core/services/chat/internal/repository/calls_groups.go`** сохраняет проверку membership для привязанных к чату звонков, а для NULL проверяет исходный participant/endpoint ACL, исходные slots и право пригласить третьего. Общие media/admission/cleanup пути используют participant binding.
- UI: `call-buttons.tsx`, `call-history.tsx`, `chat-room-view.tsx`, `person-row.tsx`, `chat-sidebar.tsx`, `chat-composer.tsx`, новый `new-call-dialog.tsx`/sheet на Base UI.
- Tests: Chat calls domain/integration, gateway policy/guard, native UI/WS/SFU/migration E2E. Migration15 уже допускает SQLNULL; новых таблиц не планируется. Constraint/index migration только при доказанной необходимости.

### Готовность

1. 1A: исходные и повторные трассы готовы. Штатный 3-party native call/End прошёл без 429 с ростом RTP, закрытием PC, завершением треков и записью в истории. Устойчивость к reload и длительному reconnect остаётся отдельной проверкой.
2. A/B без комнаты и сообщений → New call/поиск/пустая переписка → Audio/Video → Accept → реальное direct/relay media. SQL rooms0/messages0,1Call room_idNULL,2slots→0 после End, история у обоих. Повторить declined/missed/cancelled/failed/completed без создания чатов.
3. Первое сообщение создаёт1room/2members/1message; concurrent sends дают1pair. Call ID/history не переписываются. Double click/network retry идемпотентны; reused request ID с другим peer — conflict.
4. **Security:** nonparticipant не может read/accept/end/signal/credentials/admission; stale family/epoch/generation/lease, revoke/End fences сохранены. В **calls_groups.go** roomless A→group B: только разрешённый caller, сохранённые U/V и старые правила third-person eligibility, новый B≠A, cleanup barrier, no new chat, W не видит predecessor/direct history. Проверить forged target, W busy, concurrent End/replace, invitation expiry, no skipped membership у group chat. R3 и preconnect fallback работают с NULL room, чужие endpoints не допускаются.

### Риски

Часть429 может быть вызвана harness reload, а часть — реальным amplification; причины разделить. NULL room затрагивает много runtime цепочек, не только Start. Wrong fingerprint/legacy compatibility могут дать дубли или ACL bypass. На любом таком сбое1B останавливается, а1A не объявляется исправленным без повторного native прогона.

## Волна 2 — shell и представление разговора

### Входит

Утверждённая большая сцена, узкая compact-плашка, большой app view + browser fullscreen по user gesture/capability. Простая адаптивная group grid и focus существующего screen share. CallProvider/CallManager остаются в signed-in layout; presentation state expanded/compact отдельно от business FSM. Единственный устойчивый audio renderer; visual video muted, без клонирования capture при изменении вида.

Next Link/router, Back/Forward, переход в Chat/Calls/Games/Profile автоматически compact, не End/signOut. Return разворачивает shell над текущим route, не уводит назад в исходный чат. Native fullscreen close/Esc возвращает в app view, затем compact; не кладёт трубку. Unsupported fullscreen сохраняет полноценный app view.

### Не входит

Native PiP/Document PiP, WakeLock, sounds, новый shortcuts manager, RPC media-state/active-speaker анализ. Нет drag мини-окна/попапов/нового framework. Новые возможности выбора устройств/permission flow — Волна3, существующие controls работают через текущего manager.

### Файлы

`call-provider.tsx`, `call-overlay.tsx`, `remote-media.tsx`, новые `call-shell.tsx`, `call-stage.tsx`, `call-controls.tsx`, `call-mini-bar.tsx`, `call-audio-renderer.tsx`, presentation helper; `app/(app)/layout.tsx`, nav/tabbar/route integration. DB/proto не меняются. Tests: presentation/DOM и native calls UI/SFU/R3 regression.

### Готовность

- Expanded↔compact↔fullscreen и route/Back/Forward сохраняют Call ID/StartedAt, endpoint, capture/PC/Room refs и рост RTP. Нет второй аудиодорожки/источника/Room, repeated getUserMedia или hidden mute из-за remount.
- Compact в потоке над composer, не перекрывает сообщения/поле; нет call system-message, созданного только ради UI. Name/time/mic/End/Return доступны на телефоне.
- Все4размера/dark/light/long names: controls44+, safe-area, landscape header/End не обрезаны; group3/6 и screen share читаемы. End в каждом виде закрывает saved tracks/PC, room/slots.
- В Safari на iPhone владельца проверить поворот, сворачивание и Return to call. Fullscreen и навигация отдельно проверяются в браузерных прогонах.

### Риски

Перемещение video/audio DOM может дать двойной звук или остановить общий track. Общий layout не защищает от полного page reload; reload — отдельный текущий resume/cleanup сценарий. Fullscreen API не одинаков на всех устройствах; нельзя подменять отсутствующий native API ложным PASS.

### Реализация и откат

CallProvider сохраняет сессию и аудиовывод при переходах между маршрутами. В чате плашка встаёт над полем ввода, на остальных экранах она остаётся над навигацией. В ландшафте нижняя навигация возвращается после сворачивания сцены. Приглашённый в уже активную группу видит Accept и Decline. Отказ закрывает приглашение этого участника.

Откат интерфейса выполняется заменой только образа web на `axon-web:before-calls-wave2` с прежними Compose-файлами. 13 ноября владелец подтвердил поворот экрана и Return to call в Safari на iPhone. [Сценарий для Safari и iPhone](/Users/bvivg/Developer/PetProjects/axon/docs/calls-safari-ios-runbook.md)

Docker-прогоны и браузерная статистика сохранены рядом со скриншотами. [Доказательства волны 2](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/2-call-shell-2026-11-12/README.md)

## Волна 3 — состояния, ошибки и повседневное управление

### Входит

Аватары/placeholder вместо отсутствующего видео, локальный camera off/muted, корректные известные состояния SDK/track lifecycle; English labels без сырых DOMException. Incoming audio/video/group с caller/group title, Accept/Decline, video→Answer with audio без camera permission. Pending capture виден и Cancel доступен. Typed permission/device busy/no device/unsupported/peer busy/no answer/disconnected/unavailable/rate-limit outcomes и Try again/Call back.

Cancel не может отменить браузерный prompt программно: закрывается наше ожидание; поздний полученный stream немедленно stop, поздний Start компенсируется Cancel/End с тем же намерением/request ID. End имеет приоритет над device operation. Никакого скрытого включения camera при audio answer; её последующий запуск только по жесту пользователя.

Call back из истории создаёт новый roomless Call ID. Missed отображается как исход, даты через общие format helpers, Open chat отдельно. Selected mic/camera/output показаны radio/check + applying/error; sink применяется к новому output; devicechange/fallback проверены. Ошибки/приглашения сбрасываются на новый Call ID, mute/camera сохраняются при R3.

Reconnecting сохраняет shell/End/имя/StartedAt; WS и SDK восстанавливают **существующий** вызов. Navigator.onLine — подсказка, не источник истины. Resync сверяет version/endpoint/lease/revoke/End, без resurrect terminal call. При deadline — понятный End/Call back. Offline End сразу stop local; честный server cleanup pending, затем retry/reconcile/lease, не фиктивный ACK. Прежние budgets/FSM/R3 timers не увеличивать ради UI.

### Не входит

Новый remote mute/media-state RPC, вычисление P2P active speaker по audio analyser, server missed-filter, PiP/WakeLock/sounds/hotkeys. В 1:1 отсутствие видео обозначается `No video`, remote mute и `Good connection` отсутствуют. В SFU-группе `Muted` берётся из SDK, `Speaking` допускается в волне 4 по событию SDK.

### Файлы

`manager.ts`, `p2p.ts`, `livekit.ts`, media view adapter; `incoming-call.tsx`, `participant-tile.tsx`, `call-errors.ts`, `call-device-menu.tsx`, shell/controls изВолны2; `call-history.tsx`, `lib/query/calls.ts`, `lib/format.ts`. Для Answer with audio минимальные additive participant capability/negotiation поля/RPC только если нужны фактическому protocol — отдельные contract tests, без переработки backend/FSM. Existing a11y primitives Base UI/Tooltip/ScrollArea.

### Готовность

- Реальные permission denied/no device/device busy/pending Cancel; поздний capture stop; отмена/End не оставляют call/slots. Ошибка понятна без имени DOMException; Cancel/Try again/End всегда достижимы.
- Incoming video принимается с одним microphone permission, audio/RTP идёт; camera не запускается сама. Camera toggle/failed replacement/SDK events и P2P/SFU interop проверены native, причина remote placeholder подтверждена источником, не предположением.
- Direct Call back/Decline/Missed/Busy показывают правильный исход и новый Call ID; rooms0 до сообщения. Devices показывают реально выбранный input/output и не ломают mute/R3.
- Wi-Fi loss/recovery, outage beyond deadline, End/revoke during reconnect, profile/history429 и stale WS snapshots не создают второй call/media path. Все refs/slots чистые после End.
- В Safari на iPhone владельца проверить приём видеозвонка через Answer with audio, включение камеры в том же звонке, поворот и возврат через Return to call.

### Риски

getUserMedia нельзя гарантированно abort, поэтому late-result fencing обязателен. Answer with audio/camera presence могут потребовать настоящей renegotiation — это не скрытие video tag. Remote state не везде надёжен; новый backend ради красивого badge запрещён без отдельного решения. У Safari ограничения устройств/вывода должны давать ясный fallback.

### Реализация

Answer with audio использует прежний AcceptCall. Клиент запрашивает только микрофон, а при отдельном включении камеры меняет направление существующего видео в WebRTC или публикует дорожку через LiveKit. В P2P при выключенной камере собеседника используется No video. При переподключении WS сервер обновляет поколение P2P. Клиент закрывает старый транспорт, сохраняет Call ID, StartedAt и настройки микрофона и камеры. При End без сети клиент останавливает локальное медиа и повторяет запрос после resync с исходным request ID.

13 ноября завершена Docker-проверка волны 3. Пройдены P2P и LiveKit, отмена при задержанном Start и одновременно с Accept, восстановление и завершение после разрыва, отзыв сессии, чтения с тестовыми 429 и повтор устаревшего WS-события. После завершения проверенных вызовов слоты освобождены и комнаты закрыты. В Safari на iPhone проверены Answer with audio и включение камеры в том же вызове. Владелец подтвердил поворот и Return to call. VoiceOver исключён из приёмки по решению владельца. Физическая смена устройств, работа в фоне и длительный обрыв Wi-Fi на телефоне остаются непроверенными. [Доказательства волны 3](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/3-call-states-2026-11-12/README.md)

## Волна 4 — дополнительные функции звонка

После проверки волн 2–3 на iPhone владелец разрешил продолжить. В реализацию вошли пять функций ниже. VoiceOver исключён из аппаратной приёмки по решению владельца.

### Оставить в небольшом объёме

- **Video PiP** для выбранной камеры или демонстрации. Кнопка доступна только при реальном video track и поддерживаемом API, вход по жесту пользователя. Один video element/source и прежний audio owner. Закрытие PiP сохраняет вызов. Без видео или поддержки API доступна compact-плашка.
- **Screen Wake Lock**: opt-in Keep screen on в settings; один sentinel только для видимого активного звонка, release наEnd/logout/ownership loss и reacquire послеvisibility только при всё ещё активном call. Обработать late acquire послеEnd/system denial/low power. Не обходить ручную блокировку и не обещать background execution.
- **M/V** выключены по умолчанию и работают только при фокусе внутри развёрнутого звонка. Поля ввода, меню, IME и сочетания с модификаторами исключены. Настройка в Calls → Call settings действует в текущей вкладке.
- **Один короткий сигнал входящего** включается в Calls → Call settings на время текущей вкладки. Требует взаимодействия с приложением, звучит только в видимой вкладке и останавливается при Accept, Decline или End.
- **SFU active-speaker badge**, только если существующий SDK достоверно даёт событие; без автоперестановки сетки/автофокуса. Для P2P не добавлять analyser/heuristics.

### Убрать из текущего pet scope и почему

| Что убрано | Почему | Поведение вместо этого |
|---|---|---|
| Document PiP и произвольный pop-out | Второй DOM/document/focus/audio lifecycle и ограниченная совместимость слишком дороги для текущего размера | Video PiP где работает, app compact везде |
| Server missed-filter | Новые filter/cursor/security/regression ветки до реальной потребности в большой истории | Missed outcome виден в общей истории; не фильтровать только первую страницу и не называть это полной выборкой |
| Новый media-state RPC только ради remote mute | Нужны authoritative snapshots, epoch/generation/revision, retry/outbox, resync и тесты; это заметная backend-фича ради индикатора | Использовать лишь достоверные existing SDK/track signals; неизвестный remote mute не показывать как факт |
| P2P active-speaker анализ | Новая обработка звука/CPU/battery/permissions/lifecycle и ложные срабатывания без ключевой пользовательской пользы | Устойчивая сетка; SFU SDK badge при наличии |
| Глобальные shortcuts/custom remapping и shortcut End for everyone | Конфликты OS/browser/VoiceOver и случайное разрушительное действие | Scoped M/V + доступные обычные кнопки, Esc не End |
| Богатые ringback/end sounds и новые background delivery гарантии | Autoplay/notification/OS ограничения, лишние preferences/test cases | Один optional incoming tone, прежний push статус остаётся честно непроверенным |

Это отложенные функции, не упрощение admission/ACL/consent/delivery/cleanup. Если позже понадобятся, отдельная задача/APPROVE с контрактом и реальными evidence.

### Файлы

`call-pip.ts`, `remote-media.tsx`, `use-call-wake-lock.ts`, `use-call-ringtone.ts`, `call-provider.tsx`, `call-shell.tsx`, `call-controls.tsx`, `call-overlay.tsx`, `call-stage.tsx`, `livekit.ts`, `app/(app)/calls/page.tsx` и стили сцены. Возможности проверяются по API браузера. Схема данных и RPC прежние.

### Готовность

В браузере проверить PiP при сворачивании и навигации с ростом RTP, Wake Lock при скрытии и возврате вкладки, M/V в сцене и обычный ввод в чате, один сигнал при входящем и Speaking по событию LiveKit. После End проверить отсутствие PiP и аудиовыходов, закрытые соединения, завершённые треки и свободные слоты. На физических устройствах отдельно записать PASS, unsupported или pending для доступных возможностей. VoiceOver в этот прогон не входит.

13 ноября финальный веб-образ развёрнут на axon.test. В Chromium прошли прямой звонок, группа на троих и PiP на демонстрации вкладки. После End слоты освобождены, комнаты закрыты. Проверки на физических устройствах остаются открытыми. [Результаты волны 4](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/4-call-options-2026-11-13/README.md)

### Риски

Наличие API не доказывает работу на live MediaStream, особенно в Safari/PWA. iOS может прервать background capture и отклонить WakeLock. Полной гарантии всех API на всех устройствах не даём; корректный app view/compact и явный capability fallback обязательны. После failed hardware spike scope/макет пересмотреть до внедрения.

## Поддержка устройств и приёмка

| Семейство | Цель | Что реально доказано сейчас |
|---|---|---|
| iPhone 15 Pro, iOS 27.2, Safari | Версия со слов владельца | Аудио, включение камеры в звонке, поворот и Return to call проверены. Функции волны 4 на телефоне пока не проверены |
| Другие iPhone/iPad Safari | Responsive portrait/landscape, safe-area, capability fallback; Safari и Home Screen mode учитывать отдельно, если используется | Макеты, не WebRTC hardware certification |
| Android Chrome/Edge/Firefox | Touch UI и tested media path; APIs только по capabilities | Автоматические browser/viewport tests не заменяют Android device |
| macOS Safari/Chrome/Firefox | Мышь/клавиатура, WebRTC, VoiceOver | Нужны final live gates новой реализации |
| Windows/Linux Chrome/Edge/Firefox | Responsive UI/keyboard, native media на доступном stand | Не обещать проверку каждого устройства/ОС, фиксировать фактическую матрицу |

«Для всех устройств» — адаптивная базовая функциональность и честные fallbacks, а не выдуманный PASS на каждом устройстве. Layout проверяется в Docker на4заданных размерах вdark/light/long names; движки Chromium/Firefox/WebKit — где доступны. Проверка медиа проходит в настоящем браузере. Объём аппаратной приёмки фиксируется по каждой волне. iPad/Android и OS/browser версии записывать как tested/pending/unsupported, без UA-угадываний.

Для аппаратной проверки волны 4 нужен телефон с доступом к доверенному HTTPS-стенду. Docker-стенд использует имеющиеся тестовые аккаунты и сертификат.

## Остановка и откат

V3 принят. Первая правка 1A проверена, остаток по ускоренному reload записан отдельно. В любой волне остановка при ACL hole, дублированном capture/audio, ложной связи истории/чата, изменении Call ID от layout, потерянном End/cleanup или недоказанных slots0. Не отключать проверки и не растягивать deadlines.

Каждая волна использует additive contracts и отдельный feature flag/shipping image там, где нужен rollout; сохранить предыдущий image. Для отката выключить новые starts, drain текущие operations, вернуть проверенный UI/backend-compatible путь; не resurrect terminal transports, не удалять историю/комнаты и не делать destructive down16. Legacy reads roomless history проверяются до отката. Исходный dirty/staged tree/stack upgrade/CA/ключи сохраняются.

## Что остаётся открытым

Частота media-отказов в обычном использовании и длительном reconnect неизвестна. В 1:1 v3 использует нейтральное `No video`, поэтому продукт не зависит от определения remote camera-off в P2P. Базовый сценарий на iPhone проверен. VoiceOver исключён из приёмки по решению владельца. Изображения камеры иллюстративны. Push, внешний NAT, акустика, low-power и background остаются измеряемыми ограничениями. Известная пауза R3 hard-drop 9,6–9,7 с остаётся отдельной от UX-волн.

Источники ограничений: [PiP](https://developer.mozilla.org/en-US/docs/Web/API/HTMLVideoElement/requestPictureInPicture), [Document PiP](https://developer.mozilla.org/en-US/docs/Web/API/Document_Picture-in-Picture_API), [WakeLock](https://developer.mozilla.org/en-US/docs/Web/API/Screen_Wake_Lock_API), [W3C keyboard shortcuts](https://www.w3.org/WAI/WCAG22/Understanding/character-key-shortcuts/). Это ориентиры для будущих spikes, не результаты аппаратных тестов.
