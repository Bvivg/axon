# Calls

Ветка: feature/hybrid-calls. Рубежи1/2 и отдельно разрешённый Рубеж3 закрыты. План: [calls-hybrid.md](/Users/bvivg/Developer/PetProjects/axon/.claude/plans/calls-hybrid.md).

Остановки владельца после browser spike1.1 и доставки через WS1.3 соблюдены. Затем отдельно разрешены оставшийся Рубеж1, Рубеж2 и Рубеж3. Для Рубежа3 выбраны параллельная подготовка SFU без двойного звука и автоматический запуск при проблемах P2P.

Статус 2026-11-10: **Рубежи 1–3 закрыты по Docker media evidence**, основной сайт [https://axon.test](https://axon.test) запущен с автоматическим P2P→SFU при неисправности уже активного1:1. Native migration8/8, SFU6/6, UI7/7, WS5/5,19real Postgres/Redis race integration, настоящий refresh/reconnect/logout308462ms; trusted main LAN migration/3-party SFU/P2P пройдены. CALLS_E2E_PROBE=0, schema16 clean, slots0/nonterminal0. При полном искусственном обрыве P2P измерена пауза декодированного audio9,6–9,7s. [Evidence Рубежа1](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-final/README.md), [Рубежа2](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R2-final/README.md), [Рубежа3](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R3-final/README.md).

## Волна UX 1B — звонок без переписки

С 2026-11-12 можно позвонить из пустой переписки, из результатов поиска человека или через **Calls → New call**. `StartDirectCall` проверяет сессию, endpoint и адресата и сохраняет звонок в истории обоих участников с `room_id=NULL`. Первое сообщение отдельно создаёт чат. Существующая переписка продолжает использовать `StartCall` с membership ACL. При Add person сохраняются прежние правила приглашения и ограничение доступа к исходному звонку. Проверены native direct/relay в Docker, история, cleanup и отсутствие переписки до сообщения. Relay проверен на временном coturn, проверка LAN TURN на устройствах остаётся открытой. [Evidence 1B](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/1B-roomless-direct-2026-11-12/README.md).

## Волна UX 2 — экран звонка

**Full screen** разворачивает сцену в браузере. На устройствах без Fullscreen API используется полноэкранный режим внутри приложения. **Minimize call** сворачивает звонок в плашку, **Return to call** открывает сцену над текущим разделом. Переход в другой раздел автоматически сворачивает сцену. В чате плашка находится над полем ввода. В ландшафте **End** находится в шапке, **People** открывает участников и приглашения.

13 ноября владелец подтвердил поворот экрана и **Return to call** во время видеозвонка в Safari на iPhone. VoiceOver исключён из приёмки по его решению. [Сценарий для Safari и iPhone](/Users/bvivg/Developer/PetProjects/axon/docs/calls-safari-ios-runbook.md)

## Волна UX 3 — входящие звонки и устройства

Входящий видеозвонок можно принять через **Answer with audio** с доступом к одному микрофону. **Camera** запрашивает камеру отдельным нажатием. **Cancel** закрывает ожидание разрешения в Axon. Если браузер после этого всё же отдаст поток, клиент сразу его остановит.

**Devices** показывает выбранные устройства. Если браузер управляет аудиовыходом сам, меню указывает на системный выход. **Call back** в истории начинает новый звонок. **Open chat** открывает переписку. При разрыве соединения на экране остаётся текущий звонок и кнопка **End**. End сразу выключает локальное медиа и сообщает об ожидании ответа Axon, если сеть недоступна.

## Рубеж2.1 — проверка admission

Настоящие LiveKit Server v1.13.11 (digest25cd902de7cacbd5f1f779f6a6ac1eae20ba8f56b0935934ce6b0ab80d1e3d20), JS2.23.0 и Go SDK2.19.0. Сначала проверены на изолированном Docker stand. Private CheckMediaAdmission в Chat проверяет подпись/issuer/expiry, opaque endpoint identity, Call ID, выбранный endpoint/epoch/generation, текущие membership/slot/lease и Auth family. Private CheckFamily в Auth защищён внутренним ключом; app access token не хранится в LiveKit JWT. Gateway проксирует только /rtc,/rtc/validate,/rtc/v1,/rtc/v1/validate. 7880 и Server API наружу не опубликованы. В Рубеже2.2 production admission дополнительно требует SFU transport и актуальную per-member media_version; временный P2P admission probe выключен.

3/3 E2E: native legacy/v1 WebSocket join, validate200 на обоих протоколах; двусторонние RTP/audio energy/video frames, selected host/host UDP, playback unmuted; End/room removed/tracks ended/slots0. 36 отрицательных комбинаций JWT/routes, private RPC/Server API публично404, public7880 ECONNREFUSED. Длинный real-clock probe306420ms наблюдал server refresh, новые WS с обновлённым JWT и RTP после signal/full reconnect; после Logout original/refreshed JWT запрещены на4routes. Всего20 сохранённых track refs ended. Итоговая SQL isolated stand: slots0, nonterminal calls0.

Измерения revoke/removal — не SLA: последний Auth.Logout→participant removal199ms; TOCTOU с реальным TCP drop после admission позволил поздний join после End, затем verified webhook/reconciliation удалили participant. Фактическое время конкретного прогона записано в evidence/result.json (первое измерение1900ms). Нулевого окна нет; при недоступном Server API честной жёсткой верхней границы удаления нет. Это остаточное ограничение явно сохраняется, TTL не подменяет removal. [Evidence2.1](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R2-admission/README.md).

Решение владельца2026-11-04 для A→B: **только отдельный групповой звонок, без нового чата**. У B собственный participant ACL и нет привязки к старому direct room; W не получает predecessor ID/историю A. Приглашать можно существующий direct-контакт инициатора. Начатый из group chat звонок проверяет актуальный member ACL этого чата. Рубеж2.2 уже разрешён; дополнительный APPROVE не нужен.

## Рубеж2 — использование и ограничения

В существующем групповом чате Audio call/Video call запускают SFU-звонок, максимум6участников. Каждый приглашённый нажимает Accept сам. Leave call завершает только собственное участие; владелец может Remove участника или End for everyone. При уходе владельца роль получает оставшийся принявший участник. Повторное приглашение требует завершённого cleanup; новая media_version запрещает прежние JWT, поздние Ready и Cleanup ACK.

В активном1:1 Add person выбирает существующий direct-контакт. Подтверждение: “Start a group call? This will end the current call.” Busy/ACL/availability failure до транзакции сохраняет A и его медиа. После commit A завершён навсегда, B имеет новый ID и собственный ACL без нового чата. До ACK очистки обоих участников A credentials B не выдаются и W не приглашается. Отказ/ошибка B не возрождают A. История U/V содержит A и B со связью, у W только B.

Fallback до соединения сохраняет Call ID, увеличивает generation и останавливает весь P2P до выдачи SFU credentials. До deadline10s P2P должен подтвердить оба selected endpoint; затем cleanup barrier3s и SFU setup12s. При отключённом Рубеже3 ACTIVE P2P не переходит в SFU. Обычный SFU reconnect не перезапускает остальных участников; endpoint resume меняет только его epoch/media_version.

Migration15 добавляет call_transports/call_webhook_receipts, nullable room_id, predecessor/replacement связи и endpoint-user FK. На закрытии Рубежа2 main schema15 clean; после контрольных End slots0/nonterminal0. Рубеж3 добавляет migration16. SDK подписывает минимальные room grants, initial TTL120s; серверный refresh наблюдался реально. Подписанные вебхуки дедуплицируются по event ID/body hash, reconcile сверяет фактические комнаты и участников с текущим SQL/Auth. Поздний webhook не активирует терминальный звонок.

Проверено:6native SFU browser scenarios55.4s; UI7/7 и WS5/5 regressions;14race integration4.190s, включая реальный schema14→15 и запрет destructive down при истории/receipts; Go internal race/lint, Buf, web types/lint. Production refresh/reconnect/logout307093ms: fresh WS со свежим четырёхчастным JWT, рост RTP/energy/video после обоих reconnect, observed revoke-removal135ms,26retained tracks ended. Это наблюдаемое время, не hard bound при отказе Server API.

Основной LAN3-party gate10.0s: selected host/prflx UDP, audio RX +6309/+6062/+6916bytes, energy +0.0411/+0.0267/+0.0164, video +6/+5/+10frames; actual server3participants→room gone,18native tracks ended, PC closed. Физические микрофоны, Safari/iOS, screen picker/output devices и внешние NAT не проверены. **push: не проверен; внешние NAT не проверены**. Retention/GC ещё не задан; load100пар исключён из объёма.

Разработка Рубежа2 велась в ignored source copy, затем63source files перенесены после baseline hash checks, activation22ms. Обновление стека и прежний working tree сохранены; commit/push/PR нет.

## Рубеж3 — автоматическое переключение активного1:1

Сервер включает возможность только для двух selected endpoints с явной migration capability. Нативный P2P `disconnected` в течение2s или `failed` запускает одну попытку на generation/endpoint epoch. RTT/packet-loss эвристики не используются. Call ID, StartedAt, direct room ACL, slots и запись истории сохраняются. Add person по-прежнему завершает A и создаёт новый групповой звонок B без чата.

PREPARING8s сохраняет P2P источником звука. Кандидат SFU получает отдельную generation и clones текущих треков, SDK LocalAudioTrack/LocalVideoTrack переводятся в mute **до** публикации: один `native.enabled=false` недостаточен, SDK включает его при создании LocalTrack. Готовность кандидата требует непустой набор фактически созданных PC, каждый connected, outbound audio RTP и второго участника комнаты. JS SDK2.23.0 по умолчанию использует singlePeerConnection, поэтому требование ровно двух PC неверно. Это подготовка транспорта на тишине, двусторонний audio/video growth доказывается после переключения.

SDK mute при подготовке применяется только к параллельному кандидату. Обычный вход в SFU сохраняет путь публикации и native track gate Рубежа2; звуковой output выключен до ACTIVE и own media-ready. Параллельного источника медиа у обычного SFU-входа нет.

После двух Ready COMMITTING3s: сначала отключить P2P publication/output, затем включить SFU и сообщить Switched. Два Switched атомарно фиксируют primary SFU. CLEANING3s закрывает старые PC и original tracks; два ACK завершают операцию. Отсутствующий ACK завершает звонок. До commit отказ/timeout переводит в ABORTING3s: выключить SFU, дождаться SDK gate/disconnect, остановить clones и восстановить P2P. Здоровый источник остаётся ACTIVE, иначе bounded reconnect15s и End. После commit P2P не возрождается.

Mute/camera сохраняются, device/screen source replacement заблокирован во время операции; уже выполняющаяся замена отменяет подготовку. Screen clone публикуется отдельно; camera при таком переходе остаётся off, пока пользователь сам её не включит. Физический screen picker и Safari/iOS остаются ручными проверками. Пауза не обещается нулевой; browser timeline измеряет gates/outputs, а не акустическую паузу на настоящих устройствах.

Migration16 добавляет `call_migrations` и endpoint capability. Canonical state в calls.document, audit/transports/outbox обновляются одной транзакцией. UUID операции, source/target generation, монотонный serial, endpoint epoch и media_version запрещают поздние отчёты и candidate JWT после End/abort/revoke/resume. Смена endpoint до commit отменяет кандидата и использует следующее, не занятое им P2P generation; reconnect в незавершённом post-commit cleanup завершает звонок.

Финальный Рубеж3: native8/8, повтор UI2/2 после блокировки уже открытого device menu, SFU6/6, UI7/7, WS5/5 с настоящим Redis outage,19PG/Redis race integration,69web unit, Go race/lint0, Buf/types/build PASS. Во время WS gate устранена зависимость authorization от optional presence: typed ListSessions.include_presence=false для Chat.Identity и durable SQL CheckFamily. Ownership/revocation/expiry/JWT не ослаблены, обычный UI по умолчанию получает presence. E2E30s/expect10s/retries0 и rate limits сохранены. Настоящий server refresh/reconnect/logout308462ms: свежие JWT/WS и рост RTP, observed removal122ms; это не hard bound при отказе Server API.

Основной trusted LAN gate20,1s сохранил Call ID/StartedAt, подтвердил P2P host/host UDP → SFU prflx/host UDP, рост RX audio/energy/video в обе стороны,2server participants → room gone. Все16сохранённых original/clone/remote tracks ended,4PC closed. Timeline не обнаружил одновременных включённых звуковых путей. Пауза между последним ростом P2P audio и первым декодированным SFU audio9,7031/9,6289s при sampling100ms; включает native ICE detection и подготовку после полного искусственного network drop. Это измерение browser counters, физическая акустика не проверена, бесшовный переход не обещается.

Основная группа3party PASS9,6s, финальный P2P/login/refresh/End и actual /calls/probe,/calls/spike404 PASS7,0s. Первоначальный429 на входе вызван общим NAT IP тестов: дождались штатного окна, limits120general/min и10auth/min сохранены. Main12services работают, schema16 clean, незавершённых call/operations0, slots0, probe0; LiveKit7880 private, media192.168.2.1:7881TCP/7882UDP.52source files перенесены после baseline checks, activation11ms/conflicts0; main CA/ключи и прежнее дерево сохранены. [Evidence, команды и hashes](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R3-final/README.md). **push: не проверен; внешние NAT не проверены; физический Safari/iOS не проверен.**

Запуск после additive migration16: к прежним base + calls-lan + calls-sfu-lan добавить `-f core/deploy/docker-compose.calls-migration-lan.yml`. Штатный откат: `CALLS_MIGRATION_DISABLE_NEW=true` с теми же overlays, дождаться завершения current operations/End, затем выключить infrastructure flag. History/audit сохраняются; down16 отказывается удалять непустую историю. Для isolated reproduction: `bash core/deploy/calls-migration-e2e.sh`; `CALLS_VERIFY_REFRESH=1` дополнительно проверяет настоящий refresh/reconnect/revoke после handoff с отдельным12min бюджетом. Обычные E2E30s/expect10s/retries0 не меняются.

## Объём Рубежа 1

В первоначальном Рубеже 1 старт был только из существующего direct-чата. Проверка direct room и обоих участников применяется к legacy `StartCall`. Доступные сегодня входы описаны выше в волне UX 1B. Add person и блокировки не входили в первоначальный рубеж.

Evidence находится в git-ignored каталоге [.local/evidence/calls/](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls). Секреты/ключи в R1-spike/.secrets не включать в отчёт и не добавлять в git. Тестовые CA и credentials существуют только для disposable Docker-стенда.

**push: не проверен. внешние NAT не проверены. Safari/iOS hardware: не проверено.** По решению владельца Docker-прогона достаточно для сетевого гейта Рубежа1; реальная Web Push доставка не заявляется. Sender/SW, durable claim/receipt/ACK/TTL и eligibility групповых приглашений реализованы и проверены кодом/SQL.

## Spike 1.1

Два native RTCPeerConnection в настоящих browser contexts на существующем AXON web image. Origin http://localhost:3000 — настоящий loopback secure context, без insecure-origin bypass. Chromium/Firefox используют browser fake capture devices, а не mock WebRTC. Chromium получает WAV с тоном 440 Hz; Firefox — встроенный fake capture. SDP/ICE пересылает диагностический harness, не Chat WS: надёжность product signaling этим не проверяется.

Coturn 4.18.0-r0-alpine доступен только в отдельной Docker bridge-сети с default route, необходимым Firefox ICE. Listeners: 3478 UDP/TCP и 5349 TLS/TCP, relay range 49160–49220. TURN REST credentials краткоживущие, private/loopback peers запрещены с исключением только отдельной тестовой подсети 172.30.251.0/24. Публичные порты не публикуются. CA импортируется в Chromium NSS store и в каждый отдельный disposable NSS-профиль Firefox; TLS-проверки не отключаются. PulseAudio null sink обеспечивает настоящий звуковой output в Docker для Firefox Web Audio.

Команды из корня репозитория; нужен актуальный shipping axon-web-e2e-web. Browser runner собирается независимо из Node22 и текущего package-lock.json с Playwright1.64.1. Image IDs записываются в evidence, основной dev stack не пересобирается.

~~~sh
docker compose -f core/deploy/docker-compose.calls-spike.yml build init
docker compose -f core/deploy/docker-compose.calls-spike.yml run --rm playwright
docker compose -f core/deploy/docker-compose.calls-spike.yml run --rm --no-deps --entrypoint node playwright /app/e2e/calls-spike-report.mjs
docker compose -f core/deploy/docker-compose.calls-spike.yml logs --no-color coturn
docker compose -f core/deploy/docker-compose.calls-spike.yml down
~~~

Runner сохраняет native getStats до/после, фактически выбранную candidate pair, RTP packets/bytes/frames и энергию принятого audio из реального Web Audio analyser. End проверяет все сохранённые local/remote tracks: ended, PC closed, AudioContext closed, media elements удалены. После End проверяется новый native capture. Настоящий browser permission denial и недоступный microphone device проверяются отдельно.

В историческом spike1.1 бизнес-Call ID, DB room/participants и slots=N/A: backend Calls тогда не существовал. В1.3 ниже они проверены по настоящим Call IDs и SQL. Два spike endpoints не выдаются за готовый пользовательский UI.

Результат 1.1: **21/21 PASS** в Chromium156.0.8078.4 и Firefox157.0, Playwright1.64.1; timeout30s/expect10s/retries0. [Evidence](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-spike/README.md), [native stats](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-spike/result.json). Реальные selected pairs/direct/relay, двусторонние RTP/audio/video deltas, End/recapture подтверждены. Остановка1.1 выполнена, последующее разрешение1.2–1.3 получено.

Проверяются пары одного движка, не interop Chromium↔Firefox. Permission denial проверяется в Chromium; отсутствующий device ID — в обоих. Физические устройства и Safari/iOS не проверены. Сертификаты тестового CA действуют два дня: для более позднего повтора остановить только spike stack, заново создать его .secrets и запустить init; не удалять остальные evidence.

## Backend и WS, шаги1.2–1.3

Реализованы внутри Chat: endpoints, FSM RINGING→CONNECTING→ACTIVE→ENDED, membership ACL существующего direct room, request_id/fingerprint, атомарные user slots/outbox, per-endpoint/epoch ACK и resync, history API, TURN REST credentials. PostgreSQL — источник истины. Redis — доставка; SDP/ICE не сохраняются и истекают через5s. Отдельного calling-сервиса нет; LiveKit SDK добавлен только в Рубеже2.

Контракт [calls.proto](/Users/bvivg/Developer/PetProjects/axon/core/shared/proto/axon/calls/v1/calls.proto): RegisterEndpoint (включая resume), StartCall, AcceptCall, DeclineCall, CancelCall, EndCall, ReportConnection, AcknowledgeCleanup, GetCall, GetActiveCall, ListCalls, GetMediaCredentials. Gateway передаёт существующий JWT, sensitive/standard policy действует и на Calls. Busy возвращает failed_precondition, чтобы общий HTTP429 retry не повторял занятый звонок как rate-limit запрос. Int64 sequence/version/epoch/generation остаются protobuf string/BigInt.

Миграция000013 создаёт6таблиц: calls, call_participants, call_endpoints, call_user_slots, call_requests, call_outbox. Canonical Call/participants/readiness/cleanup хранится в calls.document, остальные таблицы дают FK, ACL, слоты, индексы и delivery state. Scalar state/version/id проверяются constraints. Обе recipient snapshots outbox записываются в транзакции изменения Call. ACK привязан к endpoint/epoch; один endpoint не подтверждает событие за остальные. Replay ищет все unacked, а не только sequence>cursor, и идёт batches100. Retention/GC пока не задан: request/outbox данные не удаляются.

Endpoint: server UUID, proof256bit (в БД только SHA256), user+fid, epoch, один server connection ID на epoch, lease30s. Resume увеличивает epoch и ограждает старый socket/commands. Register/mutations/credentials и heartbeat проверяют настоящую Auth family через ListSessions; при ошибке renewal запрещён. Heartbeat10s. Потерянный revocation event покрывается bounded lease; сервер не может физически остановить WebRTC у недоверенных клиентов.

Ring30s, initial setup10s, reconnect15s, cleanup ACK3s, release без ACK не позднее30s после End. CleanupExpired отделён от CleanupDone: сервер не утверждает, что аппаратные треки исчезнувшего клиента остановлены. ACTIVE разрешён после readiness обоих endpoints. SFU fallback отсутствует: setup failure завершает звонок, Retry создаёт новый Call ID.

В существующем Chat activity socket добавлены call_attach/ack/resync/heartbeat/signal. Capability только после attach: старые chat sockets не получают неизвестные call_event. Calls frame≤32KiB, SDP≤16KiB, ICE candidate≤4KiB,64 signals/s. Общий уже существующий1MiB/compression transport не ослаблен. Redis subscription-ready проверяется до Subscribe return, Calls overflow закрывает socket для resync.

Nonvisual [CallChannel](/Users/bvivg/Developer/PetProjects/axon/ui/web/src/lib/calls/channel.ts) подключён к ChatActivityProvider над routes. SDP не буферизуется после разрыва WS, ACK после consumer. В1.4–1.5 добавлены CallManager/media ownership/perfect negotiation/controls, endpoint proof в sessionStorage и Web Locks для cloned tabs; всё проверено UI regression. Diagnostic /calls/probe и /calls/spike закрываются ранним server proxy404 без CALLS_E2E_PROBE=1; один streamed notFound не гарантирует HTTP404.

## Evidence1.3

[README](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-ws/README.md), [полные stats и DB состояния](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-ws/result.json), [вывод browser tests](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-ws/browser-final.log), [команды](/Users/bvivg/Developer/PetProjects/axon/.local/evidence/calls/R1-ws/COMMANDS.txt).

5/5 PASS за28.9s, Chromium156.0.8078.4/Playwright1.64.1, timeout30s/expect10s/retries0. Native direct audio и forced TURN/TLS video проходят через реальную цепочку RPC→Chat transaction→outbox→UserSignalBus→Redis→activity WS→CallChannel. SDP/ICE обмена через Node harness здесь нет. Получены выбранные host/host и relay/relay pairs, растущие RTP/audio energy/video frames в обоих endpoints, ACTIVE/ENDED и история. Все12 local/remote track refs ended, PC/AudioContext closed; после двух Cleanup ACK реальные SQL slots2→0 и cleanup_done=true. Это проверка диагностического consumer, ещё не пользовательского CallManager.

Дополнительно: unacked replay после reconnect/epoch1→2 и отказ старому epoch; настоящий Logout закрывает конкретный activity socket, старый JWT получает403 на register/start/credentials; настоящий Redis stop/start,2committed events во время outage и доставка обоим после восстановления. Legacy chat message/ack продолжает работать. Go race unit/integration6cases с настоящими PostgreSQL/Redis проходят, gateway catalogue/Go lint/Buf/types/ESLint проходят. SQL up/down/up в отдельной схеме6→0→6 с ROLLBACK проверен; основной dev не затронут.

Chat config по default CALLS_ENABLED=false; основной LAN overlay включает true и добавляет TURN URLs/secret из ignored LAN/.secrets/lan.env. Миграции13(calls) и14(call_push, без новых таблиц) применены к основной БД. LAN web работает из shipping runtime с /api proxy; реальный .env и старые данные сохранены.

Для повтора нужны только Docker и доступные локально images/интернет к image registries; внешний login/token не нужен, users/JWT/TURN/CA генерируются для disposable stand. Запускать из корня репозитория:

~~~sh
docker compose -f core/deploy/docker-compose.calls-spike.yml build init
docker run --rm --network none --entrypoint node -e TURN_LISTEN_IP=172.30.252.20 -e TURN_ALLOWED_PEERS=172.30.252.0-172.30.252.255 -v "$PWD/ui/web/e2e:/app/e2e:ro" -v "$PWD/.local/evidence/calls/R1-ws:/evidence" axon-calls-spike-playwright:local /app/e2e/calls-spike-init.mjs
docker compose -p axon-calls-e2e --env-file .local/evidence/calls/R1-ws/.secrets/calls.env -f core/deploy/docker-compose.web-e2e.yml -f core/deploy/docker-compose.calls-e2e.yml up -d --build auth chat gateway web coturn
docker compose -p axon-calls-e2e --env-file .local/evidence/calls/R1-ws/.secrets/calls.env -f core/deploy/docker-compose.web-e2e.yml -f core/deploy/docker-compose.calls-e2e.yml run --rm playwright
~~~

CA двухдневный: при истечении обновить только .secrets disposable Calls-стенда после его остановки. Полное архивирование SQL/logs и генерация отчёта описаны в COMMANDS.txt. Browser runner использует свой shipping image, отдельную подсеть172.30.252/24 и loopback origin, host ports не публикуются. Docker socket есть только в test runner для SELECT evidence и stop/start строго Redis проекта axon-calls-e2e; приложения его не получают.

После сохранения evidence удалить только этот disposable stack:

~~~sh
docker compose -p axon-calls-e2e --env-file .local/evidence/calls/R1-ws/.secrets/calls.env -f core/deploy/docker-compose.web-e2e.yml -f core/deploy/docker-compose.calls-e2e.yml down -v
~~~

1.4–1.5 завершены: CallManager/PeerMedia/UI/history и Calls push code, native direct/relay, reconnect/revoke, multi-tab accept, permission denial, busy history, Chat restart/cleanup. **push: не проверен; внешние NAT не проверены.** Safari/iOS/physical devices, native desktop screen picker и физический output остаются manual runbook; это не аппаратные доказательства.

## Основной LAN запуск

Основной домен: [https://axon.test](https://axon.test), стандартный443. В hosts Mac добавлено127.0.0.1 axon.test; сертификат включает DNS SAN axon.test, исходный CA сохранён. Docker browser gate на этом точном origin проходит login/refresh/WS/native call/End. Системное доверие CA в macOS не установилось автоматически (SecTrustSettings authorization denied); его нужно явно подтвердить в Keychain Access, не обходя browser TLS warning.

Для iPhone запись hosts Mac не действует. CoreDNS profile dns запущен без forwarding: loopback1053 для Mac и DNS-over-HTTPS192.168.2.1:8443 для iPhone, только зона axon.test. Стандартный53 Internet Sharing не менялся: его DNS proxy не использует split правило Mac. На Mac добавлен /etc/resolver/axon.test (127.0.0.1:1053); hosts127.0.0.1 сохранён. Привилегированный bind LAN53 и общий forwarding не используются.

На iPhone с доверенным AXON LAN CA и Wi-Fi раздачи Mac скачать [AXON Local DNS](http://192.168.2.1:3444/axon-dns.mobileconfig), установить через Settings → General → VPN & Device Management, затем открыть https://axon.test. Профиль содержит только DNSSettings с SupplementalMatchDomains=[axon.test], ServerURL=https://192.168.2.1:8443/dns-query; остальные домены сохраняют текущий DNS ([Apple DNSSettings](https://developer.apple.com/documentation/devicemanagement/dnssettings/dnssettings-data.dictionary)). Профиль не подписан; не устанавливает MDM/VPN/новый CA. Установка и работа на физическом iPhone ещё не подтверждены. Откат на iPhone — удалить AXON Local DNS; на Mac — удалить только /etc/resolver/axon.test, остановить calls-dns.

DoH проверен по HTTP/2 с проверкой CA: axon.test→192.168.2.1 (NOERROR,1answer), example.com→REFUSED (0answers); загрузка профиля200, plutil lintOK. Fake OAuth в LAN overlay использует относительные /oauth/fake/authorize и /auth/callback, точечный proxy направляет authorize в auth. Это конфигурация только текущего test provider; подключение настоящих OAuth providers потребует абсолютных зарегистрированных callback URL. Настоящий Chromium прошёл fake login на IP и axon.test, callback same-origin, Secure HttpOnly cookie, без localhost redirect. Состояние входа сохраняется в исходной вкладке; после изменения начать вход заново, старые OAuth ссылки не использовать.

Локальный адрес Mac [http://localhost:3003](http://localhost:3003) также работает через API/WS proxy; этот HTTP listener опубликован только на127.0.0.1. Chromium loopback secure context позволяет microphone без установки CA на самом Mac. Друг использует LAN HTTPS и доверяет CA по runbook. Обе схемы проверены настоящим browser login/refresh/Call/Accept/End.

Сайт: [https://192.168.2.1:3443](https://192.168.2.1:3443). [Установить CA и проверить реальные устройства](/Users/bvivg/Developer/PetProjects/axon/docs/calls-safari-ios-runbook.md). Ключи не передавать: скачивается только публичный CA на3444. Проверено через отдельный Docker browser client с настоящим TLS trust, secure context, HttpOnly/Secure cookie с /api path, native call/playback/End.

```sh
docker compose -f core/deploy/docker-compose.yml -f core/deploy/docker-compose.calls-lan.yml up -d
```

Этот запуск сохраняет основной AXON, PostgreSQL/MinIO data и обновлённый стек. Выключить Calls без удаления history: CALLS_ENABLED=false в LAN overlay и recreate Chat; публичные coturn/HTTPS можно остановить отдельно. Для возврата обычного dev web использовать только основной compose и пересоздать web/gateway/chat, данные и Calls миграции сохранять. Down migrations удаляют history и не являются штатным откатом.

## Запуск и откат SFU на LAN

```sh
docker run --rm --network none --entrypoint node -v "$PWD/core/deploy/calls-sfu-lan-init.mjs:/init.mjs:ro" -v "$PWD/.local/evidence/calls/LAN-SFU/.secrets:/secrets" node:22-alpine /init.mjs
docker compose --env-file core/deploy/.env -f core/deploy/docker-compose.yml -f core/deploy/docker-compose.calls-lan.yml -f core/deploy/docker-compose.calls-sfu-lan.yml up -d
```

Генератор сохраняет существующие ключи. Auth/Gateway получают только internal key; Chat также media credentials; LiveKit только свой YAML. Файлы находятся отдельно от TLS/coturn mounts. Existing AXON LAN CA не меняется, переустановка сертификата для Рубежа2 не нужна. Public domain остаётся https://axon.test, localhost3003 сохранён. LiveKit advertise IP192.168.2.1, media7881TCP/7882UDP, private7880 не публикуется. Для другого LAN-IP нужна проверенная перенастройка media/config/cert/DNS; адреса не выбирать наугад.

Текущий shipping web собран в Docker с NEXT_PUBLIC_API_BASE_URL=/api. При недостатке storage уже собранный Next standalone был упакован тем же Node22 runtime recipe; точные команды/immutable image IDs — в evidence. Обычный source build использует Dockerfile.web target runtime. При холодном Go dev build первый startup healthcheck может истечь до готовности; дождаться healthy и повторить up, не повышать timeout ради PASS. После сна/перезапуска Docker при internal proxy200/LAN443ECONNRESET достаточно пересоздать только calls-https этим же compose.

Для отката сначала CALLS_SFU_DISABLE_NEW=true и recreate Chat с теми же overlays: новые группы/приглашения/fallback выключаются, existing calls и reconcile продолжают drain. После End/actual empty rooms/slots0 можно выключить SFU и остановить только свой LiveKit; P2P остаётся. Down15 не штатный rollback: история/transports/receipts сохраняются, пустой down/up проверен только disposable DB. A после replacement не возрождается.

CI: bash core/deploy/calls-e2e.sh проверяет R1 WS/UI, затем bash core/deploy/calls-sfu-e2e.sh —6native SFU cases. Для отдельного real-clock probe CALLS_VERIFY_REFRESH=1; бюджет12min, обычные E2E timeout30s/expect10s/retries0 неизменны. Скрипт отказывается перезаписывать fixtures уже работающего axon-calls-e2e. Общий browser config исключает только fixture-dependent Calls spec; их выполняет отдельный CI job. Удалённый GitHub CI не запускался, commit/push/PR не делались.

## Дополнительные настройки звонка

- **Picture-in-picture** доступен на видео собеседника и демонстрации экрана, если браузер поддерживает эту возможность. Закрытие окна PiP оставляет звонок активным. Из компактной плашки сцену можно вернуть через **Return to call**.
- **Devices → Keep screen on** включает удержание экрана на время текущего звонка. При скрытии вкладки оно снимается. При возврате к активному звонку браузер запрашивает его снова.
- **Calls → Call settings** содержит отдельные переключатели **Call sounds** и **Message sounds**. Они включены по умолчанию и сохраняются в браузере. Гудки повторяются до ответа или отмены. При соединении и завершении слышны короткие сигналы. Звук нового сообщения сопровождает уведомление из другого чата. Звуки приложения работают в видимой вкладке после взаимодействия с сайтом.
- Клавиши **M** и **V** для микрофона и камеры включаются отдельно на время вкладки и работают при фокусе на развёрнутой сцене.
- В группах **Speaking** показывает говорящего по данным LiveKit.
