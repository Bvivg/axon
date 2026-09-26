# Chat — живой статус: прочтение, список чатов, онлайн/«был(-а)», «печатает»

## Статус: выполнено

План записан по ходу работы, а не до неё: задача выросла из серии UX-багов
(«одна галочка вместо двух», «новое сообщение не видно в списке, пока не
откроешь чат», «онлайн не меняется»), каждый из которых по отдельности был
мелким. Здесь зафиксировано, что в итоге сделано и почему именно так.

## Контекст

После этапа 2 чат доставлял сообщения только в открытую комнату. Всё, что
вокруг неё, обновлялось по кэшу TanStack Query (`staleTime` 30 с) или вообще
только после перезагрузки:

- отправитель не видел, что сообщение прочитано;
- в списке чатов не менялись последнее сообщение и счётчик непрочитанных,
  а новый личный чат не появлялся совсем;
- «online» не менялся, пока не перезагрузишь страницу, а «был(-а) в ...» не было
  вовсе;
- статуса «печатает» не было.

Результат: всё перечисленное обновляется в реальном времени, без перезагрузки,
и отражает только то, что пользователь действительно видел.

## Решения

### Прочтение — `last_read_seq` в Postgres, живой сигнал через Redis

`MarkRead` пишет `room_members.last_read_seq` (источник истины, миграция
`000003_read_receipts`), затем публикует сигнал `read` в
`chat:room:<id>:signals`. Каждый сокет, подписанный на комнату, пересылает его
всем, кроме автора. Клиент берёт максимум из `lastReadSeq` участников и живых
сигналов. Две галочки ставятся на каждом своём сообщении, если хотя бы один
другой участник прочитал его `seq`. В группе это означает «прочитал кто-то»,
а не «прочитали все».

Читается только то, что реально попало в видимую область виртуализированного
списка, и только пока вкладка видима (`visibilitychange`), с debounce 1,5 с.

В списке чатов те же две галочки у своего последнего сообщения. `Room`
получил `optional int64 others_read_seq = 9`: максимум `last_read_seq` других
участников, считается подзапросом в `RoomsForUser`. Клиент сравнивает это
значение с `seq` последнего сообщения. Сокет списка на кадр `read`
перезапрашивает `ListRooms`, поэтому галочка становится двойной сразу, как
только собеседник прочитал. `AppendMessage` сдвигает `last_read_seq` автора
до его собственного сообщения, так что автор считается прочитавшим всё, что
написал сам.

### Список чатов — отдельный сокет на все комнаты плюс пользовательский канал

`useRoomActivitySocket` открывает второй `/ws/chat` и подписывается на все
комнаты из `ListRooms`. На кадр `message` он инвалидирует
`chatKeys.lastMessage(room)` и `chatKeys.rooms`. Новую комнату этот сокет
знать не может, поэтому в сервис добавлен пользовательский канал
`chat:user:<id>:signals` (`UserSignalBus`): первое личное сообщение
(`send` с `to_user_id`) публикует `room_added` обоим участникам. Сессия
подписывается на него до `Accept`, чтобы сигнал, отправленный сразу после
открытия сокета, не терялся.

Redis-реализация сигналов обобщена: `signalTopic` (комнатные и
пользовательские сигналы), та же схема refcount на один `PubSub` на канал в
процессе, что и у сообщений.

### Presence — ZSET соединений в Redis, `last_seen_at` в Postgres

- Ключ `presence:conns:<familyID>` — ZSET, где member — id соединения, а
  score — момент истечения. `Touch` добавляет соединение и чистит
  просроченные, `Clear` удаляет только своё соединение. Строковый ключ на
  сессию, как было раньше, при закрытии одной вкладки гасил онлайн у всех
  вкладок того же браузера.
- `Online` — `ZCOUNT key now +inf > 0`. Упавшее соединение само выпадает по
  score, TTL ключа — страховка.
- Префикс сменён с `presence:session:`, чтобы старые строковые ключи не давали
  `WRONGTYPE` во время раскатки.
- При закрытии presence-сокета chat вызывает `AuthService.UpdateLastSeen` с
  токеном пользователя. Chat не может писать в `auth.users` из-за
  schema-level изоляции. Поле `PublicProfile.last_seen_at` — optional,
  additive.
- Токен presence-сокета проверяется один раз, на подключении, поэтому сокет
  закрывается кодом 4402 за 5 с до истечения токена: `UpdateLastSeen`
  успевает уйти с валидным токеном, клиент переподключается со свежим, а
  presence при такой ротации не очищается.
- Изменение presence публикуется в `presence:changed:<userID>`. Чат-сокет по
  кадру `watch_presence {to_user_id}` (не больше 64 на сессию) пушит клиенту
  `presence {user_id}`, клиент перезапрашивает `GetUsersPublicProfiles`.

### «Печатает» — эфемерный сигнал, без хранения

Кадр `typing {room_id}` разрешён только для комнаты, на которую подписана
сессия, и уходит в тот же канал комнатных сигналов. Клиент шлёт его не чаще
раза в 3 с, пока в поле что-то набрано. Индикатор гаснет через 5 с без нового
сигнала или сразу, когда приходит сообщение этого автора. Показывается в шапке
чата (в группе с именами) и вместо превью в списке чатов.

## Файлы

| Что | Где |
|---|---|
| Сигналы (room/user) | `core/services/chat/internal/pubsub/pubsub.go`, `redis.go` |
| WS-протокол и сессия | `core/services/chat/internal/ws/protocol.go`, `handler.go` — `typing`, `watch_presence`, `read`, `presence`, `room_added` |
| MarkRead → сигнал | `core/services/chat/internal/server/handler.go` |
| Presence-сокет | `core/services/chat/internal/presence/handler.go` — ротация до истечения токена, `UpdateLastSeen`, `PublishChanged` |
| Presence-трекер | `core/shared/pkg/presence/presence.go` — ZSET соединений, change pub/sub |
| Last seen (auth) | `core/services/auth/migrations/000004_last_seen.*.sql`, `internal/{domain,repository,service,server}` |
| Контракт | `core/shared/proto/axon/auth/v1/auth.proto` — `UpdateLastSeen`, `PublicProfile.last_seen_at`; `core/shared/proto/axon/chat/v1/chat.proto` — `Room.others_read_seq` |
| Позиция прочтения в списке | `core/services/chat/internal/repository/room.go` (`RoomsForUser`), `internal/domain/chat.go`, `internal/server/convert.go` |
| Gateway | `core/services/gateway/internal/proxy/auth.go`, `internal/policy/policy.go` |
| Web: сокеты | `ui/web/src/lib/ws/{protocol,use-chat-socket,use-room-activity-socket,use-typing}.ts` |
| Web: UI | `ui/web/src/components/chat/{chat-room-view,transcript,room-list,room-list-item}.tsx`, `lib/hooks/use-page-visible.ts`, `lib/query/people.ts` |

## Как проверяется

- `make check` — unit, включая `ws_test.go` (read/typing/presence/room_added),
  `presence/handler_test.go` (ротация 4402 без очистки presence) и
  `auth/internal/service/session_test.go` (онлайн, пока открыта хоть одна
  вкладка; просроченное соединение не держит онлайн).
- `make test-integration` — `chat/integration/pubsub_test.go`: комнатные и
  пользовательские сигналы проходят между двумя инстансами через настоящий
  Redis. `chat/integration/room_test.go`: `RoomsForUser` отдаёт
  `OthersReadSeq` с точки зрения каждого участника.
- `make test-e2e` — `chat/e2e/status_test.go`: `room_added` для собеседника,
  typing и read receipt в реальном времени, пуш presence и запись
  `last_seen_at` через gateway.
- Руками, два браузера: у получателя в списке мгновенно появляются новый чат,
  «typing…», превью и счётчик. В шапке — «online», потом «typing…», после
  ухода собеседника — «last seen at HH:MM». У отправителя появляются две
  галочки, когда получатель открыл чат.

## Критерий готовности

Два человека в разных браузерах видят друг у друга новый чат, непрочитанные,
«печатает», онлайн и время последнего визита без перезагрузки страницы, а
отправитель видит две галочки только на том, что получатель действительно
прокрутил в видимую область.
