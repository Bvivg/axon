# AXON — гибридные звонки: план по рубежам

Статус 2026-11-10: **Рубежи 1, 2 и отдельно разрешённый Рубеж 3 закрыты по Docker media evidence**. Основной [https://axon.test](https://axon.test) работает: schema16 clean, migration включена, probe0, slots0/nonterminal0. Рубеж3: native8/8, SFU6/6, UI7/7, WS5/5,19race integration, настоящий refresh/reconnect/logout308462ms; trusted main LAN migration, группа и финальный P2P/diagnostic404 пройдены. При полном искусственном обрыве P2P пауза декодированного audio составила9,6–9,7s; бесшовность не заявляется. push: не проверен; внешние NAT не проверены; physical Safari/iOS не проверены. [Evidence3](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R3-final/README.md), [evidence2](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R2-final/README.md).
Остановки после1.1 и1.3 соблюдены; владелец затем разрешил1.4–1.5 сообщением «дальше тогда работай». Выполнены Calls UI/media/history/push code, финальные проверки и ранее запрошенный основной LAN-запуск.

Evidence1.3: [README](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-ws/README.md), [native stats и DB states](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-ws/result.json). Прогресс Рубежа2 — в §6; утверждённый дизайн Рубежа3 — в §9, текущие проверки — в PROGRESS.md.

LAN-запуск выполнен через Docker: https://192.168.2.1:3443, дополнительный HTTPS SAN10.80.10.122. Trusted CA/bootstrap3444, TURN3478/5349+ограниченный relay range. Реальный основной shipping web проверен двумя browser contexts из отдельного Docker клиента: login/refresh/cookie path, native call/playback/End. Физические устройства друга и внешние NAT не проверены; доверие CA устанавливается вручную по docs/calls-safari-ios-runbook.md.

Этот документ заменяет порядок реализации и ADR-5/этап 7 прежнего [graph-first аудита](/Users/bvivg/.codex/visualizations/2026/11/02/01a19cf8-7e1a-7971-a1ea-044058ea38c4/axon-calls-graph-first-audit.md). Источник фактов — актуальный working tree, включая незакоммиченные изменения и уже выполненное обновление стека. Граф остаётся картой поиска: отсутствие ребра через интерфейс или Redis не доказывает отсутствие runtime-цепочки.

Первоначальный APPROVE разрешал только Рубеж1 с указанными остановками; затем владелец отдельно разрешил весь Рубеж2 и2026-11-04 отдельно Рубеж3. Существующие изменения working tree и обновление стека сохраняются, ветка feature/hybrid-calls. Commit/push/PR запрещены. Каждый рубеж закрывается только после реальных медиа-доказательств.

Решения владельца от 2026-11-03: старт только из существующего direct-чата, кнопка в шапке; звонков по поиску, блокировок и Add person в Рубеже 1 нет. Реализация push остаётся в объёме, но если окружение не поддерживает настоящий Web Push, допустимо закрытие с явной записью «push: не проверен» в evidence и docs/calls.md. Для закрытия достаточно Docker-прогона; обязательная отметка — «внешние NAT не проверены». Evidence: git-ignored каталог .local/evidence/calls/ внутри репозитория.

## 1. Зафиксированные решения и границы

| Решение | Обязательное поведение |
|---|---|
| 1:1 | Native WebRTC, прямое соединение или coturn. Начальный режим аудио/видео выбирает пользователь. |
| Группа | Self-hosted LiveKit SFU; никаких mesh-групп из нескольких P2P. |
| ADR-1 | Calls — модуль Chat Service, схема PostgreSQL chat, отдельный контракт axon.calls.v1. Нового Go-сервиса и модуля calling не создаём. |
| ADR-5 | Миграция уже идущего разговора исключена из основного объёма. Любые prepare/switch/commit/rollback двух транспортов относятся только к необязательному Рубежу 3. |
| Третий участник | Новый групповой Call ID, связанный с законченным 1:1. Разрыв и новое подключение явно отражаются в UX; подробности в §7. |
| Неудавшееся установление P2P | В Рубеже 2 допустима последовательная попытка SFU в том же Call ID, только если разговор ни разу не стал ACTIVE. Сначала закрытие P2P и остановка всех его треков, потом SFU. |
| Источник истины | PostgreSQL: состояние, версии, участники, busy slots, идемпотентность и outbox. Redis — доставка/presence; Kafka — необязательные бизнес-события, без SDP/ICE. |
| Клиенты | Текущий Next.js, React, Connect, TanStack и UI kit. Один CallManager на уровне signed-in shell, один владелец медиа и один audio output. Новый state/UI-фреймворк не добавляется. Новые строки UI — английские. |

### Явное отступление от документации

[roadmap.md](/Users/bvivg/Developer/PetProjects/axon/.claude/plans/roadmap.md) в этапе 5, [CLAUDE.md](/Users/bvivg/Developer/PetProjects/axon/CLAUDE.md) в стеке/дереве сервисов и [infra.md](/Users/bvivg/Developer/PetProjects/axon/.claude/rules/infra.md) описывают отдельный calling-сервис — тонкий LiveKit-оркестратор. Решением владельца это **заменено модулем звонков внутри Chat**. Отдельный контейнер LiveKit остаётся медиа-инфраструктурой, не бизнес-сервисом. Пустая/зарезервированная схема calling не используется и не удаляется.

В1.2 синхронизированы roadmap.md, CLAUDE.md, infra.md и архитектурная часть AGENTS.md: старый roadmap этап помечен заменённым, Calls описан внутри Chat. Старые иллюстративные деревья не означают разрешение отдельного calling-сервиса.

Граница будущего выделения: CallsService + отдельный axon.calls.v1, узкие интерфейсы Store, Signaling и MediaProvider, бизнес-таблицы с префиксом call_. Chat владеет членством/комнатами; Calls получает его через адаптер. Выделять сервис только при доказанной потребности в независимом масштабировании/жизненном цикле. Тогда переносить данные и доступ к ACL через API/события, сохраняя Call ID, внешний RPC и клиентскую модель; не закреплять межсервисные JOIN по схемам. LiveKit SDK не должен проникать в обычные chat RPC, pubsub или компоненты сообщений. Универсальную инфраструктуру «на будущее» сейчас не строим.

## 2. Что подтверждено working tree

Обновлено2026-11-04 после Рубежа2 по рабочему дереву. Статические факты и runtime-доказательства разделены; graph не заменяет динамические цепочки через интерфейсы.

| Область | Подтверждено кодом | Ещё не подтверждено / чего нет |
|---|---|---|
| Bootstrap Chat | CallsService/Connect/expiry/outbox/push + official LiveKit provider/admission/reconcile внутри Chat. | Main Calls/SFU включены, migrations13–15 применены; отдельного calling-сервиса нет. |
| Динамическая доставка | RPC → PostgreSQL transaction/outbox → CallsPublisher → UserSignalBus → Redis → Memory → WS. Подписка ждёт Redis ready; overflow закрывает subscription/WS для resync. | Реальный stop/start Redis, active-call Chat restart и Go race прошли. Нагрузочный overflow/100пар не проверялся. |
| Формат сигналов | Typed call_attach/ack/resync/heartbeat/signal. State durable; SDP/ICE ephemeral с TTL5s, target endpoint и generation/negotiation. Calls envelope допускает отсутствие room_id. | Legacy socket действительно получает message/ack без call_event. Perfect negotiation/glare относится к CallManager1.4. |
| WebSocket | Сохранены bearer/origin/expiry/revocation и существующие1MiB/permessage-deflate. Calls frame≤32KiB, SDP≤16KiB, ICE candidate≤4KiB,64 signals/s. Endpoint proof hash+user/fid+epoch+connection binding, lease30s. | Один connection на epoch и fencing проверены. sessionStorage/Web Locks ownership и clone tab fencing реализованы; multi-tab Accept/reconnect проверены в UI. Полный reload активного вызова требует явного Resume call и не выполняет скрытый capture. |
| Авторизация | Endpoint связан с refresh family, не jti. Register, mutations, TURN credentials и heartbeat проверяют реальный Auth.ListSessions; ошибка запрещает действие/renewal. | Настоящий Logout закрывает activity socket; старый JWT получает403 на register/start/credentials. Потерянный revocation PubSub покрывается heartbeat10s/lease30s. Физическое отключение недоверенного P2P невозможно. |
| Транзакции/ACL | Current room membership или собственный ad-hoc participant ACL; sorted slot locks; fingerprint; aggregate/participants/slots/outbox одной транзакцией. Member removal атомарно отзывает call admission. |14realSQL race tests доказывают lifecycle, replay, busy replacement rollback, member revoke, receipt collision, migration guards. |
| Frontend lifetime | CallProvider/CallManager над routes, PeerMedia либо lazy SfuMedia; один output на remote stream. SessionStorage/Web Locks, epoch/generation/media_version, native teardown перед ACK. | UI7/7 и SFU6/6 подтверждают cleanup/reinvite/no-overlap/Busy/owner transfer. Hardware/Safari остаются manual. |
| Frontend доставка | useRoomActivitySocket направляет call_event в typed channel до legacy parser; SDP не буферизуется на отключённом WS. ACK после consumer, replay batches100. | Browser проверяет state/SDP/ICE/resync; diagnostic probe/spike закрыты ранним404 guard приflag0. Main UI содержит P2P и группы. |
| UI/уведомления | Direct-header Audio/Video call, incoming/active/end overlay, controls/devices, virtual history. Calls push durable claim/receipt/TTL+ACK; worker refresh/GetCall перед уведомлением. | Camera replacement/mute проверены native в Docker; реальный desktop screen picker/physical output и Web Push не проверены. |
| Данные/инфраструктура | Calls proto19publicRPC +private admission; migration13 с6таблицами,14push metadata,15transports/receipts/relations/FK. | Main schema15clean; LiveKit private7880, LAN7881TCP/7882UDP, actual3partySFU/room removal/slots0 доказаны. |
| Тесты | [Playwright config](/Users/bvivg/Developer/PetProjects/axon/ui/web/playwright.config.ts): Chromium, workers=1, retries=0, test timeout 30 s, expect 10 s, native fake media device, microphone permission. | Fake device — источник для настоящего WebRTC, не mock RTCPeerConnection. Базовый config не покрывает Firefox; в1.1 он проверен отдельным calls-spike config. WebKit/Safari/iOS hardware не проверены. |

При первоначальной подготовке плана был доступен Docker: read-only <code>docker compose ls --format json</code> вернул axon running(8) и axon-graphify running(1). Тогда ничего не перезапускалось; после APPROVE перезапущен только AXON по правилу смены ветки. Исходные вложение/аудит/репозиторий доступны. Для редактирования плана дополнительные логины и токены не нужны.

## 3. Девять таблиц: что действительно нужно

Все новые бизнес-таблицы находятся в chat. Конкретный DDL и индексы проходят review перед миграцией; ниже зафиксированы обязательные инварианты, а не уже существующая схема.

| Исходная таблица | Когда | Зачем / сокращение |
|---|---|---|
| calls | Рубеж 1 | Call ID, тип/режим, инициатор, room_id (обязателен для direct Рубежа1), FSM/state version, текущая P2P attempt/generation, ever_active, server deadlines, UTC timestamps/end reason. В Рубеже 2 — predecessor/replacement links и групповой тип. |
| call_participants | Рубеж 1 | Приглашения, ответ, выбранный endpoint, media readiness/cleanup, персональный outcome и история/ACL. Unique(call,user), без доверия к user_id клиента. В Рубеже 2 — групповые роли и актуальное участие. |
| call_endpoints | Рубеж 1 | Server-issued endpoint UUID, user/fid binding, hash resume proof, connection epoch, lease и last_seen. jti не используется как постоянный endpoint. Старый epoch/чужая семья не управляют медиа. |
| call_user_slots | Рубеж 1 | Unique user_id обеспечивает один занятый звонок, атомарные start/accept/crossed calls. Состояние releasing держит ограждение до cleanup либо ограниченного lease; незавершённый cleanup не должен навечно блокировать пользователя. |
| call_transports | **Рубеж 2** | В Рубеже 1 одна P2P-попытка хранится в calls, отдельная таблица не нужна. В Рубеже 2 — последовательные transport attempts, generation, kind/state, room name/SID, operation/deadline/reconcile. Никаких concurrent candidate/committed migration-полей до Рубежа 3. |
| call_requests | Рубеж 1 | Durable request_id + actor/scope + fingerprint + результат для Start/Accept/End и гонок. Повтор с другим payload — конфликт. Активные запросы не GC; окно после завершения фиксируется контрактом. Первичный start key дополнительно уникален в calls, чтобы GC не создавал дубликат звонка. |
| call_events | **Отдельно не создаётся** | Immutable бизнес-envelope/event_id/version записывается в call_outbox; snapshot/history берутся из calls/participants. Для pet-проекта отдельный вечный event log дублирует данные. Outbox не объявляется бессрочным аудитом; отдельный audit log потребует самостоятельной необходимости. |
| call_outbox | Рубеж 1 | В той же транзакции со state/slots: неизменяемый envelope, recipient user/endpoint или sink, claim/attempt/deadline, ACK/delivery state. Сохранение до commit и повторная доставка закрывают разрыв DB→Redis/push. Publish в Redis не считается клиентским ACK. |
| call_webhook_receipts | **Рубеж 2** | Подпись/body hash проверяются до commit; dedup event ID/SID/generation, защита от повторов/out-of-order. На P2P вебхуков LiveKit нет. |

Итого: **6 новых таблиц в Рубеже 1, ещё 2 в Рубеже 2**, а не все 9 сразу. Рубеж1 использует000013_calls и000014_call_push (поля существующего outbox, без новых таблиц); будущий Рубеж2 —000015_calls_sfu. Перед реализацией повторно проверить свободные номера, не переименовывать и не откатывать текущую 000011.

Outbox хранит бизнес-события, не SDP/ICE, токены, секрет TURN или raw credentials. SDP/ICE — краткоживущие адресные сообщения с bounded queue/size/rate/TTL; потеря требует renegotiation/resync, а не бесконечного хранения IP/SDP. История доступна только участнику в его разрешённой проекции; новый участник группы не получает историю исходного приватного разговора.

## 4. Общие инварианты и доказательства гейтов

### Доставка, FSM и безопасность

- Call ID, endpoint ID, request ID, event ID, attempt/generation, connection epoch и negotiation ID имеют разные назначения. Актор берётся из проверенных claims, не из payload. Любой signal/ACK/credentials request проверяет участие, выбранный endpoint, актуальные epoch/generation, состояние и deadline.
- Call FSM Рубежа 1: CREATED → RINGING → CONNECTING → ACTIVE → ENDED; отмена/отказ/busy/timeout/ошибка — terminal outcome, а не отдельный бесконечный транспорт. Participant и endpoint имеют собственные состояния. Обратного перехода из terminal нет. Медиа ACTIVE требует готовности обоих выбранных endpoints и серверного commit.
- State/version, participants, slots, request result и outbox изменяются одной PostgreSQL-транзакцией. Для операций с room: room lock → существующие calls в стабильном порядке → user slots по отсортированным user UUID. Для конкурирующих start использовать тот же порядок slot locks/unique conflict; не брать call lock после уже захваченных slots. Deadlock/serialization retry ограничен и идемпотентен.
- Один endpoint выигрывает accept, остальные получают answered_elsewhere. Конкурирующие звонки не занимают пользователя дважды. Потеря сокета не означает мгновенный конец: server lease/reconnect deadline ограничены и описаны. Refresh access token с новым jti сохраняет endpoint только при том же живом fid и resume proof.
- Добавить active-family preflight через существующий ListSessions на register/start/accept/credentials и периодическую перепроверку только активного звонка. Предлагаемый lease 30 s с renewal не реже 10 s, fail-closed по истечении lease. Точное измеренное остаточное окно отзыва записать в evidence; не обещать мгновенный revoke через lossy PubSub. Ошибка auth/ACL запрещает выдачу credentials.
- Outbox retry не блокирует новый terminal event ожиданием ACK от офлайн-вкладки. ACK адресован получившему endpoint/event; duplicate и stale version безопасны. На reconnect сначала subscription-ready, потом snapshot + cursor/events, затем live; гонка между snapshot и live закрывается версиями/повторной синхронизацией.
- Redis subscription-ready проверяется явно. Переполнение call-control очереди завершает соответствующее соединение с требованием resync; silent drop не допускается. Закрытие — idempotent, вне RLock текущего Memory hub; нужны publish/close race tests. Ephemeral typing не обязан становиться durable.
- Старый frontend сейчас закрывает неизвестный WS type. Нужен capability handshake calls_v1: новые call events направляются только поддерживающим endpoints, а SDP/ICE — только выбранным участникам. Обновление не ломает уже открытый старый chat tab.
- Не менять существующий default 32 KiB и chat transport limit 1 MiB ради Calls. Измерить реальные SDP в spike; если не помещаются, сначала сократить/разделить ICE сообщения, затем обосновать ограниченный call-specific budget с тестами. Никаких неограниченных payload/ice backlog.
- ICE credentials короткоживущие, выдаются авторизованным endpoints, с квотами. coturn запрещает relay в loopback/private/link-local/metadata сети с точечным разрешением только тестовой медиасети, требует authentication; relay ports ограничены. Secrets не в git/browser bundle/logs. Signaling — HTTPS/WSS вне loopback; WebRTC медиа шифруется DTLS-SRTP, SFU нельзя маркировать E2EE относительно сервера.
- End прекращает capture, remote/clone tracks, screen share, audio playback, listeners/timers и connection; затем ACK. Новый вызов не должен использовать старые треки. Сервер не может физически остановить камеру недоверенного браузера — это явно ограничение, не доказательство отсутствия cleanup.
- Нативный microphone/camera permission только после пользовательского действия; аудиозвонок не открывает камеру. У исходящего — действие Call, у входящего — Accept. Autoplay/unmute не обходится скрытым audio output; UI показывает требуемое действие.

### Единый пакет доказательств

Для каждого гейта создать отдельную папку <code>/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/&lt;gate&gt;/</code>: COMMANDS.txt с точными командами/exit codes/версией образов, redacted logs, result.json, Playwright trace при ошибке. Evidence не складывать в репозиторий с токенами или персональными данными.

| Что фиксируем | Условие прохода |
|---|---|
| Реальные браузеры/медиа | Настоящие getUserMedia, RTCPeerConnection и, с Рубежа 2, LiveKit Room. Допустим browser fake device с известной аудио/видеопоследовательностью. Подмена классов/методов медиа и имитация room запрещены. |
| ICE | Два снимка getStats: transport.selectedCandidatePairId → candidate-pair → local/remote candidates, state/nominated, candidateType, protocol и relayProtocol при наличии. Direct: ни один candidate не relay; forced relay: выбран relay, есть настоящая coturn allocation. Gathered candidate сам по себе не доказательство выбранного пути. |
| RTP/audio/video | Ненулевые дельты sent/received bytes/packets по обе стороны, framesEncoded/Decoded для видео, audio samples/energy для известного сигнала. Не ограничиваться connectionState или видимым video element. Проверить actual remote playback/unmute. |
| Room/participants | Рубеж 1: реальные строки calls/participants, выбранные endpoints/state/version; LiveKit room = N/A. Рубеж 2: дополнительно SDK Room state, room SID/generation, фактические ListRooms/ListParticipants и состояние треков. |
| End | У всех сохранённых local/remote/screen/clone track refs readyState=ended, PC closed / Room disconnected, audio элементы очищены. Проверяется каждый конец, включая отказ/таймаут/ошибку, а не только happy path. |
| Slots/очистка | SQL до/после с конкретными тестовыми Call ID: после завершения cleanup 0 занятых call_user_slots; следующий звонок успешно стартует. В Рубеже 2 — participants удалены, room пуста/удалена, worker retry подтверждён при сбое API. |
| Надёжность/ACL | Crash после commit до publish, Redis reconnect/overflow, duplicate/out-of-order ACK, две вкладки accept, End/Accept race, stale epoch/ICE/generation, logout/revoke и попытка постороннего. State восстанавливается, медиа постороннему не выданы. |

Изолированный ранний P2P spike не имеет бизнес-таблиц: room/participants/slots = N/A с явной пометкой. Он не закрывает Рубеж 1. Полный гейт рубежа обязан содержать реальные SQL/runtime цепочки.

Общие Playwright timeout=30 s, expect=10 s, retries=0 и текущие assertions сохраняются. Не «лечить» сбой повышением timeout или ожиданием одного DOM текста. Новые contexts закрывать в finally. Длинное наблюдение реального token refresh в Рубеже 2 — отдельный заранее ограниченный protocol probe, не ослабление времени/проверок существующего E2E.

## 5. Рубеж 1 — signaling + native P2P 1:1

**Цель:** два пользователя звонят друг другу с реальным аудио/видео напрямую и через coturn; адресная доставка/восстановление, история и push работают через настоящий Chat. SFU и mid-call migration в этом рубеже отсутствуют.

### Prerequisites

- Явный APPROVE только на Рубеж 1.
- Docker/Compose и образы браузеров/coturn; доступ к registry. Текущий стек уже обновлён — версии не откатывать. Перед сборкой проверить Docker disk: предыдущая проверка оставляла около 3 GB, этого может не хватить. При нехватке остановить сборку и сообщить; не удалять данные/образы соседних стеков.
- Раннему loopback spike не нужны SSH, второй IP, production DNS, публичные сертификаты или OAuth логины. Локальные тестовые auth/VAPID/TURN secrets генерируются внутри Docker и не коммитятся.
- Для честного push success нужны браузер с поддержкой Web Push, разрешение пользователя и сеть до push provider. Если окружение этого не поддерживает, владелец разрешил закрытие Рубежа 1 с отметкой «push: не проверен» одновременно в evidence и docs/calls.md. HTTP mock не считается реальной доставкой.
- Для закрытия Рубежа 1 по решению владельца достаточно Docker-прогона. Внешний хост/разные NAT не требуются; итоговая обязательная отметка — «внешние NAT не проверены». Docker bridge не объявляется доказательством работы между внешними NAT.
- Реальный Safari/iOS — ручной runbook на физическом устройстве с доверенным HTTPS origin/сертификатом, когда устройство доступно. Отдельно фиксировать «не выполнено», без заявления о совместимости.

### Порядок

**1.1. Ранний browser P2P+coturn spike до полного backend.**

Изолированный Compose profile/overlay, существующее web-приложение и browser runner. Две настоящие browser sessions, native capture/PC, SDP/ICE передаёт тестовый harness. Это временный диагностический способ обмена, не product signaling и не доказательство UserSignalBus. Не добавлять отдельное demo-приложение или state/UI-фреймворк.

Loopback secure context: runner может разделять network namespace web и открывать http://localhost:3000; проверить реальный window.isSecureContext, mediaDevices и permission prompt outcome. Если тест использует API, localhost origin разрешается только в изолированном E2E CORS config; production origin policy не расширяется. Если нужен иной origin — доверенный локальный TLS, без ignore-certificate-errors/игнорирования TLS-проверок. Текущий тестовый insecure-origin flag сам по себе не доказательство работоспособности getUserMedia на обычном пользовательском origin.

Запуски: audio direct, audio forced relay (iceTransportPolicy=relay), video direct/relay, TURN TCP/TLS на 5349 при блокировке UDP. Тестовый CA доверяется только disposable browser/container; ключи не уходят в приложение. Проверить selected pair, RTP/audio delta, permission denial/no device, ICE failure, End и повторный capture после cleanup. Начать с Chromium, затем реального Firefox в Docker; если не работает в одном, записать расхождение, а не применять browser-specific mock. WebKit engine в контейнере не считается Safari/iOS hardware.

Гейт 1.1: артефакты <code>/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-spike/</code>, direct и relay реально передают media, все треки ended. Если это не доказано, основной backend Calls не разрабатывать до выяснения причины.

Фактически выполнено 2026-11-03: **21/21 PASS** в Chromium156.0.8078.4 и Firefox157.0, Playwright1.64.1, timeout30s/expect10s/retries0. Audio/video direct, forced relay UDP3478/TCP3478/TLS5349; для TLS UDP к TURN заблокирован настоящим iptables. Выбранная пара взята из transport.selectedCandidatePairId, растут RTP/audio energy/video frames. После End все сохранённые track refs ended, PC/AudioContext closed; повторный capture проходит. Нужны default route в Docker bridge, PulseAudio null sink и явный CA import в каждый NSS-профиль Firefox, без отключения TLS validation. Пары одного движка; Chromium↔Firefox, hardware и внешние NAT не проверены. Permission denial — Chromium, отсутствующий device ID — оба движка. В историческом1.1 Room/DB participants/slots=N/A, backend тогда отсутствовал; бизнес-состояния подтверждены следующим1.3. [Evidence](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-spike/README.md), [полные stats](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-spike/result.json). Остановка1.1 выполнена, последующее разрешение1.2–1.3 получено.

**1.2. Контракты, данные, endpoints, FSM.**

Публичный axon.calls.v1: Register/ResumeEndpoint (либо эквивалентный WS handshake), StartCall(existing_direct_room_id, mode), Accept, Decline, Cancel, End, GetCall, GetActiveCall, ListCalls, GetMediaCredentials (P2P), ReportConnection. Имена окончательно закрепить additive proto review; не добавлять пустые SFU/migration RPC. Авторизация на каждой операции, пагинация истории, server UTC, стабильные error codes, request_id/fingerprint. Для JSON seq/version int64 использовать штатные protobuf string/BigInt правила, не JS Number выше 2^53.

Миграция 000013 создаёт шесть таблиц из §3, unique/FK/check constraints и worker indices. Интеграционные тесты на настоящем Postgres: slots/start races, endpoint accept winner, CAS terminal/version, outbox atomicity, retry/crash, миграция up и совместимость старого chat.

**1.3. Доставка через действующий Chat WS.**

Расширить существующий global activity socket/capability handshake. call_control + call_ack + call_resync + адресные SDP/ICE с отдельным validation; использовать UserSignalBus/Redis, не вторую постоянную call WebSocket. Исправить subscription-ready/overflow для Calls. Snapshot/replay восстанавливает состояние; старые SDP не воспроизводятся после смены generation/negotiation, требуется свежая negotiation.

Избранный endpoint держит PC, остальные получают состояние without media authority. Сокет с новым epoch ограждает старый. Runtime test должен пройти путь RPC → gateway → Chat transaction → outbox → UserSignalBus → Redis → WS → ChatActivityProvider/CallManager; тестовый manual SDP bridge из 1.1 в итоговом гейте запрещён.

Фактически выполнено1.2–1.3:12RPC в axon.calls.v1, миграция000013 и6таблиц, endpoints/FSM/slots/requests/outbox/ACK/history/TURN REST внутри Chat; typed gateway proxy и nonvisual CallChannel в существующем activity WS. Регистрация endpoint, active family, connection/epoch fencing и subscription-ready/overflow реализованы. Calls по default выключен; основной dev и его миграции не менялись.

**Гейт1.3:5/5 browser tests PASS за28.9s.** Chromium156.0.8078.4: native direct audio и relay/TLS video через настоящий RPC→PostgreSQL→outbox→Redis→WS, двусторонние RTP/audio/video deltas, выбранная ICE pair, ACTIVE/ENDED participants, все12 сохранённых tracks ended, SQL slots2→0 и история. Отдельно unacked replay/epoch1→2/stale refusal; реальный Logout→socket close/403; реальный Redis outage→commit двух событий→retry после restart→доставка обоим. Go race internal/integration6 cases, gateway contract catalogue, lint/Buf/TypeScript/ESLint — PASS. SQL up/down/up6→0→6 с ROLLBACK — PASS. [Evidence](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-ws/README.md).

Остановка после1.3 снята владельцем. **1.4–1.5 выполнены:** UI7/7, WS5/5, LAN1/1; Go race/integration/linters/types и69web unit PASS. Итог: [.local/evidence/calls/R1-final/README.md](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-final/README.md).

**1.4. P2P, UI, история, push.**

Один CallProvider над routes; переход между Chat/Calls не прерывает разговор. При смене пользователя/sign out — End и cleanup. Glare решается deterministic polite/impolite roles/perfect negotiation; ICE привязан к ufrag/negotiation, очередь ограничена. ICE restart остаётся в P2P и не подключает SFU. Режимы mute/camera, выбор поддерживаемых устройств и screen share используют текущие UI primitives; невозможность смены output/screen на платформе отражается явно. Permissions/ошибки видны пользователю.

До изменения UI повторить согласованный визуальный объём двумя пунктами: появляются Call/Accept/End controls, incoming/active overlay и история; текущая навигация/чат/layout остаются. Новые строки, например “Calling…”, “Incoming call”, “Connecting…”, “Call ended”, — английские.

История: distinct missed/declined/cancelled/busy/failed/ended, длительность только с ACTIVE, курсорная пагинация, разрешённая проекция. Push: durable outbox job, TTL не дольше оставшегося ring deadline, dedup by Call ID/event, перед показом/Accept актуальная проверка; service worker не открывает media. Не подавлять звонок только потому, что user Visible=true: учитывать ACK конкретных endpoints. Истёкший/отменённый push не возрождает звонок.

Для setup предлагается P2P deadline 10 s после accept, общий ring deadline 30 s и bounded reconnect 15 s после уже ACTIVE. Это начальные проверяемые параметры, не доказанные оптимальные значения; не менять их ради зелёного теста. В Рубеже 1 setup timeout завершает звонок с ясной ошибкой и Retry создаёт новый Call ID; никакого скрытого SFU.

**1.5. End, ревокация, полный гейт.**

Cleanup ACK максимум 3 s; при недоступном endpoint сервер завершает бизнес-состояние и помечает releasing до истечения его bounded lease (не более 30 s), после чего освобождает slot. На повторном подключении stale endpoint обязан очистить старое media до нового вызова. Нельзя доказать hardware cleanup отсутствующего/недоверенного браузера по одной записи БД; такой кейс помечается как lease recovery, а не «все треки остановлены». Для живых тестовых клиентов гейт требует фактическое ended и slot release сразу после ACK.

### Файлы Рубежа 1

«Существующие» означает файлы исходного working tree. Часть новых файлов уже создана в1.1–1.3, оставшаяся UI/push часть требует снятия остановки; текущий статус указан выше.

| Существующие | Изменение после APPROVE |
|---|---|
| [Chat main](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/cmd/chat/main.go), [config](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/config/config.go), [server](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/server/handler.go) | Wiring Calls, lifecycle workers, TURN settings/health, public Connect registration. Не добавлять новый Go service. |
| [ws/handler.go](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/ws/handler.go), [ws/protocol.go](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/ws/protocol.go), [pubsub.go](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/pubsub/pubsub.go), [redis.go](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/pubsub/redis.go) | Capability/endpoint handshake, адресные calls, ACK/resync, subscribe-ready и bounded overflow. |
| [repository.go](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/repository/repository.go), [identity.go](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/identity/identity.go), [events.go](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/events/events.go), [webpush.go](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/notify/webpush.go) | Транзакционные adapters, active family check, бизнес-события и TTL-aware push sender; message flow не ослаблять. |
| [gateway main](/Users/bvivg/Developer/PetProjects/axon/core/services/gateway/cmd/gateway/main.go), [policy.go](/Users/bvivg/Developer/PetProjects/axon/core/services/gateway/internal/policy/policy.go), [wsproxy.go](/Users/bvivg/Developer/PetProjects/axon/core/services/gateway/internal/wsproxy/wsproxy.go) | Calls RPC allowlist, WS budget/forwarding только при доказанной необходимости, ACL/rate/origin tests. |
| [app-shell.tsx](/Users/bvivg/Developer/PetProjects/axon/ui/web/src/components/nav/app-shell.tsx), [chat-activity.tsx](/Users/bvivg/Developer/PetProjects/axon/ui/web/src/components/chat/chat-activity.tsx), [use-room-activity-socket.ts](/Users/bvivg/Developer/PetProjects/axon/ui/web/src/lib/ws/use-room-activity-socket.ts), [protocol.ts](/Users/bvivg/Developer/PetProjects/axon/ui/web/src/lib/ws/protocol.ts) | Глобальный lifecycle/канал Calls; не создавать параллельный call socket. |
| [clients.ts](/Users/bvivg/Developer/PetProjects/axon/ui/web/src/lib/connect/clients.ts), [chat-room-view.tsx](/Users/bvivg/Developer/PetProjects/axon/ui/web/src/components/chat/chat-room-view.tsx), [calls/page.tsx](/Users/bvivg/Developer/PetProjects/axon/ui/web/src/app/(app)/calls/page.tsx), [sw.js](/Users/bvivg/Developer/PetProjects/axon/ui/web/public/sw.js) | Calls client, действия звонка, история, push deep link/state recheck. |
| [docker-compose.yml](/Users/bvivg/Developer/PetProjects/axon/core/deploy/docker-compose.yml), [Dockerfile.web](/Users/bvivg/Developer/PetProjects/axon/core/deploy/Dockerfile.web), [.env.example](/Users/bvivg/Developer/PetProjects/axon/core/deploy/.env.example), [Makefile](/Users/bvivg/Developer/PetProjects/axon/Makefile), [CI](/Users/bvivg/Developer/PetProjects/axon/.github/workflows/ci.yml) | Coturn opt-in profile, disposable browser fixtures/команды и calls jobs. Существующие версии/сервисы обновлённого стека не откатывать. Основной dev stack не перезапускать ради spike. |

Новые:

- <code>/Users/bvivg/Developer/PetProjects/axon/core/shared/proto/axon/calls/v1/calls.proto</code>; штатная генерация в <code>/Users/bvivg/Developer/PetProjects/axon/core/shared/gen/go/axon/calls/v1/</code> и <code>/Users/bvivg/Developer/PetProjects/axon/ui/web/src/gen/axon/calls/v1/</code>, generated code вручную не менять.
- Созданы <code>/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/calls/domain.go</code>, <code>store.go</code>, <code>service.go</code>, <code>proto.go</code> и meaningful unit tests. FSM в domain.go, worker/TURN в service.go; пустые state/worker/turn scaffolds не создавались.
- Созданы <code>/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/repository/calls.go</code>, <code>calls_jobs.go</code>, <code>calls_binding.go</code>; server/calls.go, ws/calls.go, pubsub/calls.go, gateway/internal/proxy/calls.go. Notify/calls.go ещё не создан.
- <code>/Users/bvivg/Developer/PetProjects/axon/core/services/chat/migrations/000013_calls.up.sql</code> и <code>000013_calls.down.sql</code>.
- Созданы <code>/Users/bvivg/Developer/PetProjects/axon/ui/web/src/lib/calls/channel.ts</code>, manager.ts, p2p.ts и query/calls.ts. Device/screen lifecycle находится в PeerMedia, отдельного пустого media-devices.ts нет. Gated /calls/probe остаётся nonvisual диагностикой для нижнего WS gate.
- Созданы <code>/Users/bvivg/Developer/PetProjects/axon/ui/web/src/components/calls/call-provider.tsx</code>, call-overlay.tsx, call-buttons.tsx и call-history.tsx. Controls/devices внутри overlay; новый UI/state framework не добавлен.
- <code>/Users/bvivg/Developer/PetProjects/axon/core/deploy/coturn.local.conf</code>, <code>/Users/bvivg/Developer/PetProjects/axon/core/deploy/docker-compose.calls-spike.yml</code>, <code>/Users/bvivg/Developer/PetProjects/axon/core/deploy/docker-compose.calls-e2e.yml</code>. Overlay наследует shipping web-e2e stand, ограничивает ports/credentials только своим project name.
- <code>/Users/bvivg/Developer/PetProjects/axon/core/deploy/Dockerfile.calls-test</code>: disposable Go test image на текущей обновлённой версии Go с C toolchain и CGO для -race. Обычный tools go сейчас Alpine без compiler; его нельзя считать готовым race runner. Глобальный tools image/обновление стека ради этого не переделывать.
- Созданы <code>/Users/bvivg/Developer/PetProjects/axon/ui/web/e2e/calls-media-spike.spec.ts</code>, calls-permissions-spike.spec.ts, calls-spike-support.ts, calls-spike-init.mjs/report.mjs; calls-ws.spec.ts, calls-ws-support.ts, calls-ws-report.mjs и playwright.calls-ws.config.ts. Настоящие DB integration: <code>/Users/bvivg/Developer/PetProjects/axon/core/services/chat/integration/calls_test.go</code>. UI/recovery tests полного1.5 появятся после продолжения.
- <code>/Users/bvivg/Developer/PetProjects/axon/docs/calls.md</code>, <code>/Users/bvivg/Developer/PetProjects/axon/docs/calls-safari-ios-runbook.md</code>. Синхронизация трёх конфликтующих документов — как в §1.

### Приёмка и команды

Выполненные команды1.2–1.3 приведены ниже; точные exit codes, fixture generation, image IDs и логи сохранены в evidence/R1-ws/COMMANDS.txt. Только Docker, запуск из корня репозитория. Существующий tools go/vet/линтеры не заменялись; для -race используется отдельный CGO runner с настоящими Testcontainers.

~~~sh
docker compose -f core/deploy/docker-compose.tools.yml run --rm buf lint
docker compose -f core/deploy/docker-compose.tools.yml run --rm golangci-lint golangci-lint run ./services/chat/... ./services/gateway/...
docker compose -f core/deploy/docker-compose.calls-test.yml run --rm go go test -race ./internal/...
docker compose -f core/deploy/docker-compose.calls-test.yml run --rm go go test -race -tags=integration ./integration -run 'TestCalls|TestConcurrentCalls|TestOnlyOneEndpoint|TestOutbox' -count=1
docker compose -p axon-calls-e2e --env-file .local/evidence/calls/R1-ws/.secrets/calls.env -f core/deploy/docker-compose.web-e2e.yml -f core/deploy/docker-compose.calls-e2e.yml run --rm playwright
~~~

Gateway internal tests и TypeScript/ESLint также прошли; точные targeted команды в COMMANDS.txt. Это overlay из двух Compose файлов; отдельного go-check/integration service внутри browser overlay нет. Дополнительный docker-compose.calls-test.yml содержит go runner. Cleanup ограничен явным project name axon-calls-e2e и его disposable volumes, без broad down/prune.


Финальный гейт R1: direct + forced relay с двусторонним audio/video, реальный application signaling, reconnect/resync после Redis/Chat interruption, multi-tab accept race, bounded revoke и история. Push проверяется настоящей доставкой либо отмечается «push: не проверен», если окружение её не поддерживает, — это разрешённое владельцем исключение. Внешние NAT не проверены, Docker-прогона достаточно. Идёт отдельная DOM/text-проверка controls/history; screenshot нужен только для layout, не вместо stats. После End все треки ended, slots освобождены, новый вызов успешен. Hardware Safari/iOS отмечается в runbook фактически проверенными устройствами/ОС, невыполненные строки остаются невыполненными.

**Условия остановки:** обязательные паузы после 1.1 и 1.3 до подтверждения владельца; media spike не прошёл, неясен secure context/relay, runtime envelope теряется, нарушен ACL/slot invariant, не удалось доказать cleanup/revoke или не хватает Docker disk. Неподдерживаемый настоящий Web Push допускает только явно согласованную отметку «не проверен», внешний NAT не является блокером Docker-гейта. Не ослаблять тест/timeout, не писать SFU «в обход».

**Откат:** до применения — удалить только новые Calls изменения с сохранённым baseline, не revert всего working tree. После применения — выключить Calls capability/новые starts, drain/End активные Calls, остановить только calls workers/coturn profile. Аддитивные таблицы оставить для истории и расследования; down только на пустой disposable БД после проверки зависимостей. Обычные Chat/auth/upload остаются работоспособны. Откат не означает восстановление медиа уже завершённого звонка.

## 6. Рубеж 2 — ранний admission spike, затем SFU/группы/fallback

**Цель:** группы работают на настоящем self-hosted LiveKit; допуск проверяется на каждом signaling join/rejoin, а не только при выдаче JWT. Неустановившийся P2P последовательно пробует SFU. Работающий 1:1 не мигрирует.

### Prerequisites

- Рубеж 1 закрыт пакетами реальных доказательств и отдельный APPROVE на Рубеж 2.
- Зафиксировать **LiveKit Server v1.13.11 + JS SDK 2.23.0** (image digest и lockfile), не latest. Go SDK — совместимый официальный клиент, первоначальный кандидат v2.19.0; установить и проверить его только в этом рубеже.
- Доступны private LiveKit API/signaling, public gateway signaling route, media UDP/TCP и тот же coturn. Один публичный IP достаточен. Порт 7880/Server API не публикуется для обхода admission; gateway не проксирует RTP.
- Для local spike хватит Docker; для внешнего стенда нужны один DNS/IP, доверенный TLS для app/signaling/TURN и firewall-доступ. Эти данные сейчас не проверены. Проверить reachable media path до реализации групп.
- Применена R1 схема, есть настоящие endpoint/fid/lease/participant данные для admission; не заменять их mock Store.

### 2.1. Admission proxy spike — первый SFU-код, обязательный stop/go

Текущий прогресс: pinned LiveKit image digest25cd902de7cacbd5f1f779f6a6ac1eae20ba8f56b0935934ce6b0ab80d1e3d20, JS2.23.0 lockfile, официальный Go SDK2.19.0. Private SessionControl.CheckFamily в Auth дополняет private CheckMediaAdmission в Chat: актуальная family проверяется без хранения app access token в LiveKit JWT. Binding восстанавливается из подписанных room и opaque endpoint identity, не из изменяемой metadata. Временный probe допускает R1 fixtures только под CALLS_MEDIA_ADMISSION_PROBE, не является разрешением SFU для обычного P2P.

2.1 пройден: Docker E2E3/3, настоящие legacy/v1 WS и validate200 обоих протоколов, native audio energy/RTP/video и actual playback, selected host/host UDP, room2participants, End/tracks ended/slots0/room removed. 36 отрицательных JWT/route комбинаций, public Server API/private RPC404 и TCP7880 ECONNREFUSED. Real-clock306420ms: server refresh, новые WS со свежим JWT и RTP после signal/full reconnect, original/refreshed JWT403 после Auth.Logout.20saved track refs ended; final SQL slots0/nonterminal0. Go regression +8real integration, Buflint, Go lint0issues, TypeScript/targetESLint PASS.

Revoke→removal199ms; поздний join после уже выполненного admission воспроизведён TCP drop, затем удалён webhook/reconcile (первое наблюдение1900ms, последний результат в evidence/result.json). Это ненулевое измеренное окно, не жёсткая гарантия при отказе Server API. Evidence .local/evidence/calls/R2-admission. Владелец2026-11-04 выбрал отдельный B без нового чата (§7); вопрос закрыт, дополнительный APPROVE на2.2 не требуется. Рубеж2 в целом ещё не закрыт.

Сделать минимальный gateway proxy + private типизированный CheckMediaAdmission RPC в Chat и test-only probe, использующий настоящий LiveKit Room. Это разрешённая реализация только spike после APPROVE Рубежа 2; остальные SFU handlers/group UI пока не пишутся. Для минимального live media использовать существующие R1 calls/participants и явные test fixtures допуска, не объявлять это готовыми группами.

Проверить на указанных версиях и сохранить network trace:

1. Проверка JWT подписи/issuer, room/identity, Call ID, endpoint, fid, generation, lease, состояния и актуального ACL в Chat. Токен не получает roomAdmin и произвольные права. Server API — отдельный внутренний путь, не public passthrough.
2. Обычный SDK join и ошибки validate. В tagged server есть **/rtc, /rtc/validate, /rtc/v1, /rtc/v1/validate**; прокси обязан покрывать все доступные пути и действительно используемые SDK fallback routes. Не ограничиваться проверкой upgrade только на /rtc и не переписывать протокол LiveKit самостоятельно. [Server routes v1.13.11](https://github.com/livekit/livekit/blob/v1.13.11/pkg/service/rtcservice.go), [JS signaling v2.23.0](https://github.com/livekit/client-sdk-js/blob/v2.23.0/src/api/SignalClient.ts).
3. Настоящий reconnect/resume и полный rejoin после обрыва signaling/media; SDK-generated token refresh и повторное подключение с полученным от сервера JWT. Проверять по актуальному server-side допуску даже при валидной подписи/новом exp. Refreshed token не должен терять непрерывную привязку к call/endpoint/generation; если теряет, надо доказать безопасное восстановление по opaque identity/room, а не довериться metadata клиента.
4. После End, kick/removal, отзыва family, expiry endpoint и смены generation: старый и refreshed JWT запрещены на validate/join/reconnect. Неверный room/identity/чужой endpoint, forge, обход напрямую 7880 и альтернативным route отвергаются. Не отключать auth/CORS/TLS ради probe.
5. Устранить TOCTOU между admission и реальным participant join: подписанный webhook + периодический RoomService reconciliation обнаруживает опоздавший join после revoke и удаляет его. Измерить остаточный промежуток до удаления; если требуется нулевое окно, а он остаётся, это stop/решение владельца. Однократная проверка до WS upgrade сама по себе не закрывает допуск на всю жизнь сокета.
6. Реальный двусторонний RTP/audio, selected pair, room SID/participants, RemoveParticipant и room cleanup; после End — stopped tracks и 0 slots по общей evidence-матрице.

Длинное наблюдение refresh: отдельный Docker probe с заранее объявленным лимитом 12 min, реальными часами и настоящим SDK/LiveKit, без fake timers. Зафиксировать только redacted exp/nbf/opaque identity и момент приёма refresh, не raw JWT. Если за лимит refresh не наблюдался — «не доказано», выяснить протокол, не выдать обычный reconnect со старым токеном за refresh. Текущие 30 s E2E timeout/retries/assertions не изменяются.

Новые файлы **только spike**:

- <code>/Users/bvivg/Developer/PetProjects/axon/core/services/gateway/internal/livekitproxy/proxy.go</code> и <code>proxy_test.go</code>; wiring в существующих gateway main/config/policy.
- <code>/Users/bvivg/Developer/PetProjects/axon/core/shared/proto/axon/calls/v1/internal.proto</code> с private CheckMediaAdmission, <code>/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/server/calls_media.go</code>, <code>/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/calls/admission.go</code>.
- <code>/Users/bvivg/Developer/PetProjects/axon/core/deploy/livekit.local.yaml</code>, расширение calls-e2e overlay только своим livekit service/ports/private secrets.
- <code>/Users/bvivg/Developer/PetProjects/axon/ui/web/e2e/calls-admission.spec.ts</code>, <code>/Users/bvivg/Developer/PetProjects/axon/ui/web/scripts/calls-admission-refresh.mjs</code>. Если SDK нужен page bundle — закрытый test-only probe в <code>/Users/bvivg/Developer/PetProjects/axon/ui/web/src/app/(app)/calls/spike/page.tsx</code>, недоступный вне E2E flag; это не новый пользовательский экран.
- Lockfiles/chat go.mod меняются только ради настоящих SDK; не повторять завершённый stack upgrade.

Планируемые Docker-команды (пока файлов/services нет):

~~~sh
docker compose -p axon-calls-e2e -f /Users/bvivg/Developer/PetProjects/axon/core/deploy/docker-compose.calls-e2e.yml run --rm playwright npx playwright test e2e/calls-admission.spec.ts
docker compose -p axon-calls-e2e -f /Users/bvivg/Developer/PetProjects/axon/core/deploy/docker-compose.calls-e2e.yml run --rm playwright node scripts/calls-admission-refresh.mjs
~~~

Пакет <code>/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R2-admission/</code> содержит allow/deny matrix для каждой URL/операции, packet/media stats, refresh claims/timestamps, фактическое окно revoke→removal, отсутствие публичного API bypass.

**Если схема не работает — остановить Рубеж 2 до групп/fallback.** Предложить владельцу альтернативу: короткий TTL выдаваемого JWT (начальный кандидат 120 s), запрет новой выдачи после revoke, RemoveParticipant и durable retry/reconciliation. Отдельно показать остаточный риск:

- На self-hosted RemoveParticipant не инвалидирует уже выданный JWT; expiry не отключает уже подключённого участника.
- Сервер может выдавать refreshed JWT с более долгим сроком. Документация описывает срок минимум 10 min и периодическое обновление. Поэтому «TTL 120 s = максимум 120 s несанкционированного доступа» неверно. [LiveKit token lifecycle](https://docs.livekit.io/frontends/reference/tokens-grants/).
- Для имеющегося набора токенов окно повторного входа — max(exp) минус время revoke + измеренный clock skew; при повторном входе/дальнейшем refresh оно может продлеваться. Прерывание активного media ограничено задержкой RemoveParticipant/reconcile, но при недоступном API не имеет честной жёсткой верхней границы.
- Если запретить все пути повторного входа альтернативой не удаётся, назвать это ослаблением гарантии и **ждать отдельного решения владельца**. Самостоятельно принимать риск или заявлять безопасный admission запрещено.

### 2.2. Данные, LiveKit orchestration и группы — только после зелёного 2.1

Итог реализации:63source files перенесены из `.local/evidence/calls/R2-workspace` после baseline hash checks, conflicts0, activation22ms; main schema15 clean. Native6/6 PASS55.4s: новый ad-hoc B, group-chat start/leave, real network-fault fallback, Remove/reinvite/Logout, Busy third с сохранением A/RTP, original caller Leave→Decline→Accept и slots0. Дополнительно14real Postgres/Redis race integration PASS4.190s, R1 UI7/7 PASS36.4s и WS5/5 PASS25.4s. Media-version fence не позволяет старым Ready/Cleanup ACK подтвердить повторное участие; legacy request fingerprint сохранён golden test.

Production real-clock307093ms: настоящий четырёхчастный refresh, свежие WS и RTP/audio/video после signal/full reconnect, server3participants, observed revoke-removal135ms, refreshed JWT403 на4routes,26retained refs ended. Main trusted https://axon.test3party PASS10.0s: host/prflx UDP, audio +6309/+6062/+6916bytes, energy +0.0411/+0.0267/+0.0164, video +6/+5/+10frames, server room удалён,18refs ended/PCclosed/SQLslots0. Probe flag возвращён0; ранний routing guard нужен для настоящего404, поскольку streamed notFound сам может вернуть200. Физические устройства/внешние NAT/реальный push не выдаются за проверенные.

Миграция 000015: call_transports + call_webhook_receipts; расширение calls/participants и индексы/unique для predecessor/replacement и групп. Уже существующие P2P-попытки переносить только по известным данным, не выдумывать историю завершённых transports. R1 columns не удалять, additive rollout.

Calls создаёт комнату/opaque participant identities и ограниченные credentials через официальный Go SDK, принимает проверенные подписанные вебхуки, идемпотентно reconcile room/participants. Room created/participant connected не заменяют бизнес-Accept и не делают звонок ACTIVE без медиа. Вебхуки могут опоздать/повториться/не прийти; outbox и worker обязаны довести End/remove до фактического room cleanup.

Группы: Create/Join/Leave/End с явным invite/admit. Для звонка в chat room текущий member ACL проверяется при invite/accept/credentials/admission; удаление member/LeaveGroup в одной транзакции запрещает допуск и создаёт remove job, затем private API физически удаляет участника. Нет автоматического camera/microphone publish новым участником. Старт с двумя соединёнными участниками допустим; добавление/уход третьего не превращает транспорт обратно в P2P. Ограничить группу небольшим явным лимитом (начально 6), это продуктовый предел, не обещание измеренной capacity.

Новые/расширяемые файлы после spike:

| Файлы | Назначение |
|---|---|
| <code>/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/calls/livekit.go</code>, <code>groups.go</code>, <code>fallback.go</code>, <code>webhooks.go</code> в том же каталоге | SDK adapter, новые group operations, последовательный fallback, verified webhook/reconcile. |
| <code>/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/repository/call_transports.go</code>, <code>call_webhooks.go</code>; существующие calls.go/calls_jobs.go и [room.go](/Users/bvivg/Developer/PetProjects/axon/core/services/chat/internal/repository/room.go) | SFU attempts/dedup, атомарные ACL/revoke и replacement slots. |
| <code>/Users/bvivg/Developer/PetProjects/axon/core/services/chat/migrations/000015_calls_sfu.up.sql</code>, <code>000015_calls_sfu.down.sql</code> | Две таблицы + additive relations/constraints; проверить номер перед началом. |
| Ранее созданные calls.proto/internal.proto, Chat Calls handler/main/config и gateway proxy | Group APIs, webhook route, безопасный public signaling/private API. |
| <code>/Users/bvivg/Developer/PetProjects/axon/ui/web/src/lib/calls/livekit.ts</code>, <code>media-kind.ts</code>, <code>/Users/bvivg/Developer/PetProjects/axon/ui/web/src/components/calls/remote-media.tsx</code>; manager/provider/overlay/history и src/proxy.ts | SDK lazy import, per-participant outputs, текущий UI kit; раннее закрытие diagnostic routes. Room не дублирует владельца audio/capture. |
| <code>/Users/bvivg/Developer/PetProjects/axon/ui/web/e2e/calls-sfu.spec.ts</code>, <code>calls-lan.spec.ts</code>, <code>calls-sfu-report.mjs</code>, <code>/Users/bvivg/Developer/PetProjects/axon/core/services/chat/integration/calls_groups_test.go</code> | Единый6-case native SFU suite, trusted LAN, real PostgreSQL ACL/slots/receipt/migration tests. |
| Ранее созданные calls Compose, docs/calls.md, Safari/iOS runbook, scoped CI jobs | Pins/ports, эксплуатация и реальные evidence paths. Нет production overlay. |

### Приёмка, stop, rollback

Пакет <code>/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R2-final/</code>: реальные 3 участника, audio/video counters растут у всех, действительный selected pair и room participants; invite/busy/decline/leave/remove/logout/reconnect/refresh; replacement и fallback по §7–8 без параллельных transport tracks; End удаляет participants/room, все треки ended, slots=0. Повторы/потеря webhook/private API failure не возвращают terminal call в ACTIVE. Проверки §4 и подходящие Go/web regression повторяются.

~~~sh
bash /Users/bvivg/Developer/PetProjects/axon/core/deploy/calls-sfu-e2e.sh
CALLS_VERIFY_REFRESH=1 bash /Users/bvivg/Developer/PetProjects/axon/core/deploy/calls-sfu-e2e.sh
~~~

**Остановить:** любой admission bypass/непроверенный refreshed JWT, непонятное окно revocation, SFU недоступен по настоящему media path, overlap старых/новых треков, несогласованные slots/ACL, недоказанный cleanup. Не переходить в optional migration ради исправления.

**Откат:** выключить новые group starts/fallback, завершить/очистить SFU calls, сохранить историю/receipts и outbox до отработки remove jobs; оставить проверенный P2P Рубежа 1. Остановить только свой LK profile после drain, не открывать прямой 7880 для «временного обхода». Down 000015 допустим только на disposable/пустых данных с проверкой зависимостей; default — additive tables остаются. Завершённые исходные звонки replacement не возрождаются.

## 7. Третий участник: новый звонок с разрывом

Решение владельца2026-11-04: создаётся только отдельный групповой звонок B, **новый групповой чат не создаётся**. B не привязывается к старому direct room: room_id=NULL, собственные call_participants задают ACL. История/ID predecessor A доступны только U/V; W получает только B. Приглашение W разрешено инициатору из существующего direct-контакта; добавление W в исходный direct room запрещено. Для звонков, начатых из существующего group chat, room_id сохраняется и текущий member ACL продолжает проверяться.

Исходный A — ACTIVE 1:1 между U и V; инициатор U выбирает W.

1. UI до запроса: **“Start a group call? This will end the current call.”** и действие **“Start group call”**. Закрытие выбора через Add person сохраняет текущий звонок без запроса. Нельзя обещать бесшовное добавление.
2. RPC StartGroupCallFromCall(A, W, request_id), только Рубеж 2. Проверить живой endpoint инициатора, право приглашать, поддержку SFU и доступность/slot W. Если эти preconditions не выполнены до commit — A продолжается, показать причину; B не существует.
3. Транзакция с единым lock order: проверить A всё ещё ACTIVE и без replacement; создать **B с новым Call ID**, kind=group, predecessor_call_id=A; A сделать ENDED с reason=REPLACED_BY_GROUP и replaced_by_call_id=B. Один replacement на A защищён unique constraint. Атомарно перенести/reserve slots U,V из A в B и занять W; если W успел стать busy, rollback всей транзакции, A остаётся ACTIVE. Повтор/два конкурентных запроса возвращают тот же B либо конфликт, не два group calls.
4. B сначала RINGING/waiting_cleanup, без SFU credentials/media. U и V получают End A, закрывают P2P, останавливают **все** старые local/remote/screen tracks, очищают audio и ACK с актуальной media_version. U видит новый Group call/Calling…, V — Call ended, затем отдельное Incoming call B после barrier. W до barrier не получает приглашение/media. Разрыв заранее объяснён подтверждением “Start a group call? This will end the current call.”.
5. Только после cleanup ACK обоих выбранных endpoints A: B → RINGING, начинается ring deadline. U считается принявшим по явному подтверждению шага 1; V и W получают **новое** приглашение и явно нажимают Accept. Никого не присоединять/не включать камеру автоматически. Каждый принявший получает свежие tracks и SFU-only credentials; у U P2P уже закрыт. B становится ACTIVE после двух реально соединённых согласившихся участников; приглашение третьего может оставаться ringing до своего deadline.
6. До ответа V/W пользователь может отменить B. При cleanup timeout, потере lease или ошибке создания SFU B завершается failed/cancelled и освобождает slots после cleanup/lease expiry; A остаётся ENDED. UI показывает Call ended и ошибку операции, если она получена клиентом; новый1:1 создаётся вручную с новым ID. Автоматического rollback медиа в A нет.
7. Если один приглашённый отказался, другой может принять: SFU-группа с двумя остаётся SFU. Без двух connected к deadline B завершается с точным outcome. Незавершённые invitations/slots освобождаются согласно terminal state.
8. История U/V содержит две отдельные записи и разрешённую ссылку “Continued in a group call”. W видит только B; predecessor ID/данные A ему не выдаются. Длительность A заканчивается на его End, длительность B начинается на собственном ACTIVE; пауза не включается в разговор.

Гейт: зафиксировать разные IDs A≠B, связь/идемпотентность, остановку A tracks **до первого** захвата/публикации B, room B/participants и slots transfer/release; отдельно End A vs replacement, W busy race, отказ/таймаут V и failure LiveKit. Создание B — новый разговор с разрывом, не замаскированный Рубеж 3.

## 8. Fallback до начала разговора: тот же Call ID, последовательно

Только Рубеж 2. Оба приняли звонок C, attempt P — P2P, состояние CONNECTING. Параметры первоначально: P2P deadline 10 s, cleanup deadline 3 s, SFU connect deadline 12 s; все укладываются в общий setup budget 30 s. Это рабочие начальные настройки, их надо измерить, а не повышать ради теста.

1. Server activation barrier: endpoints сообщают транспортную готовность; до server commit ACTIVE/ever_active=true разговор не включается — outgoing media gates и remote audio/video output закрыты. Если ACTIVE уже committed, даже если клиент потерял его event, ветка fallback запрещена. Это исключает решение «сервер не видел ACK, хотя разговор уже шёл».
2. Если к deadline ever_active=false, сервер CAS-операцией закрывает P и ограждает его generation/negotiation, отправляет AbortP2P. Бизнес-Call ID **C сохраняется**, новый attempt S получает новую generation и kind=sfu.
3. Клиенты немедленно закрывают PC, stop всех capture/remote/clone tracks P, снимают handlers/timers/audio и отправляют CleanupAck(epoch, P). Не выполнять параллельный preconnect/Room.connect и не сохранять capture clone «для плавности».
4. Только после ACK обоих выбранных endpoints сервер допускает S и выдаёт SFU credentials; клиенты заново вызывают capture и Room.connect. UI остаётся **“Connecting…”**, при необходимости **“Trying another connection…”**; дополнительного действия пользователя/искусственного backoff нет. «Сразу» означает сразу после обязательной очистки, не одновременное включение транспорта.
5. Если cleanup ACK не пришёл в срок, endpoint lease потерян или SFU не соединился — C завершается failed, slots освобождаются по cleanup/lease. Старый P не оживает, автоматического циклического P2P↔SFU нет.
6. Если C хотя бы раз был ACTIVE, ICE failure допускает только bounded P2P reconnect/ICE restart, затем End/ручной новый вызов. Автоматический SFU после ACTIVE — Рубеж 3, здесь запрещён.

Гейт: намеренно блокировать P2P достижимость сетевыми правилами/реальным отсутствием маршрута, не mock PC. Доказать ever_active=false, P counters/track refs остановлены до S capture, один C и две последовательные generations; настоящий SFU RTP/room и cleanup. Нужен отдельный race test deadline vs media-ready/ACTIVE: ровно одна ветка выигрывает. Успешный P2P остаётся P2P, timer не переключает его позднее.

## 9. Рубеж 3 — необязательная миграция уже ACTIVE разговора

**Разрешён владельцем2026-11-04 сообщением «APPROVE Рубеж3».** Дополнительно выбраны параллельная подготовка SFU без двойного звука и автоматический запуск при проблемах P2P. Рубежи1/2 остаются отдельными пройденными гейтами.

**Цель:** сохранять один Call ID при автоматическом переключении активного1:1 из неисправного P2P в SFU, с параллельной подготовкой и одним включённым звуковым путём. Старый этап7/ADR-5 реализуется только здесь, после отдельного APPROVE.

**Prerequisites:** Рубежи1/2 закрыты реальными evidence, admission доказан на pinned versions, владелец выбрал автоматический trigger и параллельную подготовку. Дизайн ниже зафиксирован до кода. Для rollout обязательны real-browser gates и baseline hash checks; одного unit-теста недостаточно.

**Файлы:** calls/domain.go, service.go, proto.go; новые calls/migration.go, migration_service.go, migration_test.go; repository/call_migrations.go и расширения calls.go/calls_media.go/call_transports.go/call_webhooks.go/call_requests.go; config/main, typed CallsService RPC/proxy/policy и generated Go/TS. Auth: server/handler.go, server/internal.go, service/session.go, calls_family_test.go, optional ListSessionsRequest.include_presence; Chat identity/identity.go. Клиент: manager.ts, p2p.ts, livekit.ts, channel.ts, новый migration.ts и English status в call-overlay.tsx. Проверки: integration/calls_migration_test.go, e2e/calls-migration.spec.ts, calls-lan-migration.spec.ts, long-refresh script и Compose/runner Рубежа3.

**Данные:** additive000016_call_migrations: endpoint supports_migration и отдельный audit/coordinator call_migrations. Existing calls.document хранит canonical FSM/transport serial/operation/peer fences. Source/target attempts используют call_transports; outbox и audit фиксируются в той же SQL transaction. call_events не добавляется; destructive down с историей запрещён.

**Приёмка:** дополнительно к §4 — один business call, реальные RTP/energy/video до и после, timeline gates без двойного audio, stale fencing, timeout/commit/rollback/revoke/resume races, сохранённые native track/PC refs, slots0/room gone после End. Параллельная подготовка одобрена владельцем; её звуковой output выключен. Browser timeline измеряет gates, а не акустику физического устройства. Пакет <code>/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R3-final/</code>; reproduction `bash core/deploy/calls-migration-e2e.sh`, `CALLS_VERIFY_REFRESH=1` для actual server refresh после handoff.

**Остановка:** отсутствие отдельного APPROVE, недоказанный admission/ownership, неустранимое дублирование audio, потеря ACL или неопределённый commit. **Откат:** выключить только migration capability; возвращаться к проверенному поведению Рубежа 2 — новый group call с разрывом и sequential preconnect fallback. Не пытаться самовольно resurrect terminal transport. Данные/история сохраняются.

### Принятый дизайн реализации Рубежа3

- Внешние логины/SSH не потребовались: Docker, pinned SDK images и локальные generated keys доступны. Разработка и первые проверки выполнены в `.local/evidence/calls/R3-workspace`, снятой с актуального дерева, baseline647files. Затем52файла перенесены после hash checks, activation11ms/conflicts0. Main LAN CA/ключи и обновление стека сохранены. На финальном прогоне Dockerfree3,6GiB; чужие images/volumes не удалялись.
- Только уже ACTIVE direct P2P с двумя selected endpoints, оба явно поддерживают migration. Нативное disconnected2s либо failed запускает автоматический RequestCallMigration; один durable operation на call, конкурентные запросы сводятся к нему. Высокий RTT/packet loss сами по себе не используются как непроверенная эвристика.
- PREPARING8s: P2P остаётся владельцем audio/output. SFU подключается к отдельной target generation с клонами текущего capture, выключенными публикацией/воспроизведением. Это одобренное владельцем перекрытие подготовки; не скрытый второй микрофон. Device/screen source replacement во время операции блокируется, mute/camera сохраняются. У screen clone отдельный source; камера не включается незаметно.
- Candidate-ready требует непустой набор фактически созданных native PC, каждый connected, рост outbound audio RTP и второго участника комнаты. В pinned JS SDK2.23.0 default singlePeerConnection=true: требование двух PC было ошибочным и исправлено. SDK mute применяется до publish только к параллельному кандидату; обычный SFU использует проверенный путь Рубежа2.
- Два candidate-ready → COMMITTING3s. Каждый клиент сначала gate(false) P2P и убирает его output, затем включает SFU и подтверждает switched. Локально никогда не включены два аудиопути. До второго switched старый PC не закрывается — возможен rollback. Пауза не обещается нулевой и измеряется.
- Два switched → durable commit: тот же Call ID/StartedAt/history/slots, transport=SFU, generation=target. CLEANING3s: закрыть старый PC и остановить все original track refs; два cleanup ACK → COMPLETED. После commit исходный transport не возрождается. Неподтверждённый cleanup после commit завершает call, а не объявляется успешным.
- Failure/timeout до commit → ABORTING3s: сначала выключить/отсоединить SFU и остановить clones, затем восстановить P2P output и ACK abort cleanup. Если P2P исправен — прежний ACTIVE; иначе bounded reconnect15s, затем End. Неподтверждённый abort cleanup → End. После неудачи не делать бесконечный auto-retry на одном endpoint epoch.
- Operation UUID, монотонный transport serial, source/target generation, выбранные endpoint epochs и candidate media_version ограждают поздние ready/switch/cleanup/abort, refresh и resync. Смена endpoint во время незавершённой операции прерывает её; пропущенные поколения не переиспользуются. End/revoke всегда имеют приоритет и инвалидируют candidate admission.
- Новая additive migration16: endpoint capability и audit/coordinator call_migrations; canonical state остаётся в calls.document. Call/transports/outbox/coordinator обновляются одной SQL transaction; scalar constraints и FK не заменяются JSON без валидации. Existing R1/R2 reads работают при migration flagfalse/schema15. Откат — запрет новых migrations, drain current operations, затем выключить capability; down только disposable/empty.
- UI English: Optimizing connection… / Switching connection…; без нового framework и без новой group/chat при автоматическом1:1 handoff. Add person по-прежнему создаёт B≠A по Рубежу2.
- До rollout нужны real-browser success/abort/End/revoke/reconnect и реальные network faults, RTP/audio-energy/video counters, selected ICE, timeline отсутствия двойного audio, сохранение ID/StartedAt, slots и cleanup обоих PC/track sets. Docker E2E30s/expect10s/retries0 не менять. Недоказанное seamless/аппаратную Safari/iOS работу не заявлять.

### Результат Рубежа 3 — 2026-11-10

Native8/8 покрывают automatic handoff, healthy rollback, End при prepare/commit, missing cleanup, mute/camera/reconnect/revoke, endpoint resume и bounded recovery без повторной автоматической попытки. Повтор двух UI-сценариев после последней правки2/2 подтвердил блокировку уже открытого device menu. Регрессии SFU6/6, UI7/7, WS5/5 с настоящим Redis stop/start;19PG/Redis race integration,69web unit, Go race/lint0, Buf/types/build пройдены. Длинный production probe308462ms наблюдал настоящий refresh, новые WS/JWT и RTP после reconnect; Logout removal122ms — наблюдение этого запуска, не SLA.

WS outage выявил лишнюю зависимость авторизации от Redis presence. Chat.Identity теперь запрашивает typed optional include_presence=false, Auth.CheckFamily проверяет durable SQL напрямую. Ownership/revocation/expiry/JWT остаются обязательными, обычный ListSessions по умолчанию возвращает presence. Таймауты, rate limits и assertions не ослаблены.

На основном доверенном https://axon.test сохранены Call ID/StartedAt; selected ICE P2P host/host UDP, SFU prflx/host UDP. Двусторонние audio RX/energy/video растут до и после handoff. Два server participants → room gone;16сохранённых native tracks ended,4PC closed, slots0/nonterminal0, незавершённых операций0. Timeline не обнаружил одновременных включённых audio paths. Пауза между последним ростом P2P audio и первым декодированным SFU audio9,7031/9,6289s, sampling100ms; включает native ICE detection и подготовку после полного network drop, физическая акустика не измерялась.

Основной3-party SFU gate9,6s и финальный P2P/login/refresh/End/actual diagnostic404 gate7,0s пройдены на shipping image. Первый финальный вход получил429 из-за общего NAT IP тестов: дождались штатного окна и повторили, limits120general/min и10auth/min сохранены. Main12services running, schema16 clean, probe0, LiveKit7880 private, media192.168.2.1:7881TCP/7882UDP. [Команды, raw stats и hashes](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R3-final/README.md). Физический iPhone, внешние NAT и push остаются непроверенными.

## 10. Урезанный инфраструктурный объём

- Один coturn с authentication: 3478 UDP/TCP, 5349 TCP/TLS и небольшой ограниченный relay range. Для внешнего relay всё равно нужен корректный public/external IP mapping и открытые relay ports; наличие только двух listener ports недостаточно.
- Один self-hosted LiveKit node в Рубеже 2; signaling через существующий gateway, media напрямую клиент↔LiveKit/coturn. Порты media mux/TCP выбрать из проверенной конфигурации и проверить reachability; не считать HTTP health доказательством медиа.
- **Caddy, второй публичный IP, TURN/TLS на 443, production-overlay и нагрузка 100 пар исключены.** Нет скрытой задачи production HA/cluster/Kubernetes.
- 3478/5349 могут блокироваться корпоративной сетью. Такой пользователь в этом объёме может не подключиться; не обещать универсальный обход без 443. SFU fallback тоже зависит от достижимости его медиа.
- Достаточно функциональных 2-party P2P/3-party SFU прогонов, нескольких повторов cleanup и гонок. Это не нагрузочная/capacity сертификация. Небольшой product limit группы не объявляется измеренным пределом сервера.
- Safari/iOS: отдельный **ручной** runbook — HTTPS/cert trust, permissions/autoplay, mic-only, camera flip, device loss, screen ограничения, background/lock, interruption, direct/relay, End/track cleanup, reconnect. Фиксировать версию устройства/ОС и результат. Docker WebKit не заменяет физическое устройство.
- Не внедрять новый UI kit, state framework, отдельный demo-server, production ingress или TURN control plane. Адаптер WebRTC shim допустим только по измеренной несовместимости, не как автоматическая новая зависимость.

## 11. Слабые места и непроверенные допущения

| Риск / допущение | Что известно сейчас и как закрыть |
|---|---|
| Admission на pinned SDK/server | 2.1 доказал legacy/v1 join/validate и CORS.2.2 доказал production SFU binding с4частями, per-member version, настоящий refresh/reconnect/revoke и6native UI scenarios; main LAN3party прошёл. Server API/public7880 закрыты. |
| Revoke race | Перед upgrade остаётся TOCTOU: реальный TCP drop воспроизвёл late join после End, webhook/reconcile удалили его. Последнее время в R2-admission/result.json; Auth.Logout removal199ms. Это наблюдения, не нулевое окно и не hard bound при отказе Server API. TTL не подменяет removal. |
| P2P/browser trust | Spike1.1 подтвердил native getUserMedia/ICE/direct/relay/cleanup в Chromium и Firefox на Docker-стенде. Hardware и interop разных движков не проверены. Сервер не контролирует hardware capture недоверенного клиента. |
| Network Docker vs Internet | Direct/relay на bridge не доказывает разные NAT/IPv6/корпоративный firewall. По решению владельца Docker-гейта достаточно; итоговая отметка «внешние NAT не проверены» сохранена, внешний хост не блокирует Рубеж1. |
| Token/family lease | Actual Auth family проверяется также private RPC при admission; trusted revocation истекает endpoint leases. Native capture после настоящего Logout останавливается, SDK participant удалён. Потеря PubSub покрывается heartbeat/lease и reconcile; при отказе Server API hard bound физического удаления отсутствует. |
| Отзыв уже работающего P2P | Прямое медиа не проходит через Chat: backend не может принудительно разорвать PC двух недоверенных клиентов. Lease/revoke останавливает штатные клиенты и запрещает новый signaling/credentials; это ограничение native P2P, не гарантия физического отключения по JWT. |
| Redis ready/overflow | Ready handshake/close-on-overflow, Go-race и настоящий Redis outage с durable outbox PASS. Native active call пережил настоящий Chat restart в UI regression. Stress на переполнение/100пар не выполнялся. |
| Push/autoplay | Наличие sender/SW не доказывает доставку. Provider/browser permissions/сеть и user gesture требуют живого прогона; неподдерживаемый браузер — явный результат, не silent success. |
| Direct call ↔ chat room | Решено владельцем: Рубеж 1 стартует только из существующего direct room с проверкой обоих участников; звонков по поиску, blocklist и Add person нет. Backend не принимает произвольный peer_id вместо room ACL. |
| Резервирование slots при replacement | Предлагаемый break-before-make сохраняет взаимное исключение, но может оборвать A до отказа V/W или падения SFU. Это сознательная UX-цена простоты, явно показанная до Start group call; бесшовность не обещается. |
| Activation barrier | P2P/SFU tracks gated до ACTIVE и own media-ready. Native fallback закрывает старые tracks до SFU, только never-active call. Pure FSM/realSQL tests ограждают deadline, generation, повторные MediaVersion Ready/Cleanup и split invitation/setup deadlines. Клиентская Ready — заявление выбранного endpoint; физическое медиа дополнительно доказано browser stats, не доверяется как серверная аппаратная телеметрия. |
| Оптимальные deadlines/лимит 6 | Это проектные стартовые значения, не измеренные UX/capacity. Изменение требует причины/нового evidence, не увеличения test timeout после падения. |
| История/GC | Запросы/outbox обязаны переживать активный звонок и reconnect; точное retention/период history purge ещё не задано. Не удалять данные до фиксации retention и теста late retry/terminal resync. |
| Safari/iOS | Физических аппаратных доказательств нет. Ручной runbook сохраняется; нерешённые пункты честно остаются открытыми. |
| Регрессия обновлённого стека | Ранее общий browser suite дал 47/48, один registration timeout; три preflight отдельно прошли 3/3 без изменения timeout. Это отдельная известная нестабильность, не причина ослаблять calls tests и не медиа-evidence. |
| Ресурсы Docker | Перед1.2 было20.7GiB свободно после очистки владельцем; независимые runner/shipping images собраны без новой очистки. Сервисные данные и соседние stacks не трогались. |

## 12. Что изменено относительно прежнего плана и текущая готовность

- Основной объём разделён на два независимо принимаемых рубежа; mid-call migration вынесена в необязательный третий с отдельным APPROVE.
- Третий участник создаёт B≠A с явным окончанием A и связанной историей; preconnection fallback сохраняет Call ID, но требует полного teardown старой попытки. В обоих случаях нет скрытого параллельного медиа.
- В Рубеже 1 сначала настоящий browser/coturn spike, затем backend. В Рубеже 2 admission/refresh spike предшествует группам и fallback; при неудаче нужен выбор владельца.
- Шесть таблиц вместо девяти в первом рубеже; transports/webhook receipts отложены, события объединены с outbox без отказа от durable delivery/ACL/slots.
- Отдельный calling-сервис, Caddy, второй IP, TURN/TLS443, production-overlay и 100 пар исключены; Safari/iOS оставлены ручным runbook.
- Факты working tree отделены от runtime-доказательств. R1-final, R2-admission и R2-final пройдены; отдельно разрешённый R3-final8native cases, actual refresh308462ms,19race integration и trusted main LAN migration/3party/P2P/diagnostic404 также пройдены. Реальный push и hardware не выдаются за проверенные.

Подтверждены native media через пользовательский UI и Chat signaling, FSM/history/busy, ACK/resync/Redis recovery, auth revocation, concurrent Accept, Chat process restart, slots/cleanup, основной LAN HTTPS login/refresh/call. Native desktop screen picker/physical audio devices, Safari/iOS и реальные устройства друга остаются явными ручными проверками.

**Рубежи 1–3 закрыты; основной LAN running, schema16 clean, diagnostic routes404, probe0. При полном искусственном P2P drop измерена audio-пауза9,6–9,7s. push: не проверен. внешние NAT не проверены. Physical Safari/iOS не проверены. Commit/push/PR не выполнялись.**


### Итог1.4–1.5 и остаточные проверки

UI7/7 (native direct/relay TLS, microphone/camera, camera replacement, navigation/history, End/redial, reconnect/revoke, cloned tabs/Accept winner, permission denial, Busy, настоящий Chat restart); WS5/5 (ACK/resync/fencing/Redis outage); основной trusted LAN1/1. Source/commands/stats/slots/participants доступны в R1-final и LAN evidence. Push persistence добавлен migration000014_call_push поверх call_outbox; новые SFU таблицы начинаются с000015. Реальная доставка Web Push не доказана — разрешённая отметка сохранена. Safari/iOS, физические устройства, screen picker/physical audio output не выдаются за проверенные; ручной runbook заполнен честными статусами. CI Calls job выполняет обе application suites; исходный общий browser suite сохраняет прежние tests.

