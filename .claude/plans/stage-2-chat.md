# Этап 2 — Chat

## Контекст

Этап 1 закрыл сквозной путь «браузер → gateway → сервис → Postgres» для запросов
типа «запрос-ответ». Чат нужен, чтобы тем же способом закрыть **realtime**:
соединение живёт минутами, сообщения идут в обе стороны, а инстансов сервиса
может быть больше одного. Всё, что здесь будет отлажено — протокол поверх
WebSocket, fan-out между инстансами, восстановление после разрыва — переиспользует
этап 3 для ходов в играх. Поэтому чат идёт раньше игры, хотя игровой домен уже
написан: отлаживать realtime на домене с правилами дороже, чем на домене, где
сообщение — это строка.

Результатом считается: два браузера в одной комнате видят сообщения друг друга
без перезагрузки, разрыв соединения не теряет историю, и всё это гоняется в CI.

## Что уже есть

`shared/pkg/kafka` и `shared/pkg/ws` написаны заранее (см. `stage-2-transport.md`)
и ждут первого потребителя:

- `ws.Accept` / `ws.Dial` / `ws.DialRetry` с backoff, keepalive, лимитом кадра и
  correlation ID, который переживает handshake.
- `kafka.Producer` / `kafka.Consumer` с топиками `<domain>.<event>` и группой,
  выведенной из имени сервиса.

Инфраструктуры под чат нет вообще: ни Kafka в compose, ни схемы `chat`, ни
модуля.

## Решения

### Источник истины — Postgres, а не сокет

Сообщение считается существующим после коммита в Postgres. Всё остальное —
доставка. Из этого следует порядок в обработчике `send`: проверить членство →
записать → опубликовать в Redis → ответить `ack` → опубликовать в Kafka. Клиент,
получивший `ack`, знает, что сообщение переживёт падение любого инстанса.

У каждого сообщения есть `seq` — монотонный `bigint` из последовательности. Это
курсор: клиент, вернувшись после разрыва, просит `since=<последний seq>` и
получает пропущенное. UUID для этого не годится, а `created_at` не годится тем
более — два сообщения в одну миллисекунду ломают порядок.

### Fan-out — Redis, восстановление — Postgres

Инстанс держит только свои сокеты. Записав сообщение, он публикует его в канал
`chat:room:<room_id>`; каждый инстанс, у которого есть подписчики этой комнаты,
рассылает его своим. Потеря Redis означает потерю доставки в моменте, но не
потерю данных: клиент переспросит историю по `since` и догонит (`rules/infra.md`
— всё в Redis должно быть восстановимо).

### Kafka — событие для чужих доменов, не транспорт для своего

`chat.message` публикуется после коммита и не участвует в доставке до
собеседника. Потребителей пока нет — это событийный контракт на будущее
(уведомления, аналитика), и он проверяется integration-тестом, а не тем, что
кто-то его читает. Fan-out через Kafka был бы вторым способом делать то же самое
с худшей задержкой.

### WebSocket терминируется на gateway

Браузер открывает сокет к gateway, gateway дозванивается до chat вторым сокетом
и перекладывает кадры. Причина — `rules/security.md`: gateway это граница
доверия, на нём CORS, rate limiting и correlation ID, и второй публичный вход в
обход всего этого пришлось бы обвешивать теми же тремя вещами заново.

Токен браузер положить в заголовок не может, поэтому он едет в
`Sec-WebSocket-Protocol`: `axon.chat.v1, axon.bearer.<access token>`. Gateway
проверяет его своим уже существующим JWKS-верификатором и **перевыставляет
настоящим `Authorization: Bearer`** на внутреннем сокете. Chat проверяет токен
сам, локально, как проверял бы на Connect-вызове — то есть не верит gateway на
слово, чего `rules/security.md` и требует. Заголовок с готовым `user_id` был бы
именно тем доверием, которого там нельзя.

Токен живёт 15 минут, соединение — дольше. Chat закрывает сокет кодом
`4401 token expired`, когда срок истёк; клиент обновляет токен и переподключается
с тем же `since`. Альтернатива — рефреш внутри сокета — это второй механизм
аутентификации рядом с существующим.

*Отвергнуто:* публиковать WS-порт самого chat наружу. Короче на один хоп, но
дублирует CORS, лимиты и origin-проверку в каждом realtime-сервисе, а их будет
минимум два.

### Протокол — JSON, subprotocol `axon.chat.v1`

Не protobuf: `rules/api-contracts.md` прямо выносит realtime за пределы Connect,
а версия в имени subprotocol даёт ту же возможность эволюционировать, что и
пакет `v1` в контракте. Кадры:

| Направление | Кадр |
|---|---|
| → | `{"type":"subscribe","room_id":"…","since":123}` |
| → | `{"type":"unsubscribe","room_id":"…"}` |
| → | `{"type":"send","room_id":"…","client_id":"…","body":"…"}` |
| ← | `{"type":"message","room_id":"…","id":"…","seq":124,"author_id":"…","body":"…","sent_at":"…","client_id":"…"}` |
| ← | `{"type":"ack","client_id":"…","seq":124}` |
| ← | `{"type":"error","code":"…","reason":"…","client_id":"…"}` |

`client_id` — UUID, который придумывает клиент. Он же делает повторную отправку
после разрыва идемпотентной: уникальный индекс `(room_id, author_id, client_id)`,
конфликт возвращает уже записанное сообщение вместо второго.

### Авторизация уровня ресурса — внутри chat

Членство проверяется в chat на каждый `subscribe` и каждый `send`, а не только
при входе в комнату: gateway не знает, кто в какой комнате состоит, и знать не
должен.

## Шаги

Один шаг — один коммит.

1. **Контракт.** `core/shared/proto/axon/chat/v1/chat.proto`: `CreateRoom`,
   `ListRooms`, `GetRoom`, `JoinRoom`, `LeaveRoom`, `ListMessages` (страница
   истории по курсору). Отправки сообщения здесь нет — она в WS.
2. **Модуль и хранилище.** `core/services/chat/` (`go.mod`, запись в `go.work`),
   миграция `000001_init` в схеме `chat`: `rooms`, `room_members`, `messages`
   (с `seq bigserial` и уникальным `(room_id, author_id, client_id)`), домен и
   репозиторий.
3. **Connect-хендлер и сервис в compose.** `internal/server/handler.go`,
   `cmd/chat/main.go` (только wiring), `/healthz`, `/readyz`, `/metrics`; сервис
   `chat` и `migrate-chat` в `docker-compose.yml`; proxy и policy для
   `ChatService` на gateway.
4. **Kafka в compose и продюсер.** `confluentinc/confluent-local:7.5.0` — тот же
   образ, что уже поднимают integration-тесты, чтобы брокер в проекте был один.
   Публикация `chat.message` после коммита. (Этим закрывается открытый вопрос
   roadmap про Kafka vs Redpanda: выбран Kafka-совместимый confluent-local ради
   единственного брокера на проект.)
5. **WebSocket в chat.** `internal/ws/`: кодек кадров, локальный hub подписчиков,
   верификация токена через JWKS, проверка членства, запись через сервисный слой.
6. **Redis fan-out.** `internal/pubsub/`: публикация в `chat:room:<id>` и
   подписка; два инстанса chat доставляют сообщение друг другу.
7. **WS на gateway.** `internal/wsproxy/`: апгрейд, извлечение токена из
   subprotocol, верификация, дозвон до chat через `ws.DialRetry`, перекладывание
   кадров в обе стороны, закрытие обеих сторон по любому разрыву.
8. **Web.** `ui/web/src/app/(app)/chat/[roomId]/page.tsx`, компоненты в
   `components/chat/`, транспорт в `lib/ws/useChatSocket.ts` (`react-use-websocket`
   с явным backoff), история — TanStack Query, длинный список — TanStack
   Virtualizer. Рендеринг отдельно от транспорта.
9. **Тесты.** Unit: кодек кадров, правила членства, дедуп по `client_id`.
   Integration: репозиторий против Postgres, fan-out между двумя инстансами через
   Redis, публикация `chat.message` в Kafka. E2E: два клиента в одной комнате,
   сообщение от A доходит до B; разрыв и повторное подключение с `since` не
   теряет историю.
10. **Браузерный e2e.** Playwright: два контекста в одной комнате, сообщение
    появляется у второго без перезагрузки.

## Файлы

| Что | Где |
|---|---|
| Контракт | `core/shared/proto/axon/chat/v1/chat.proto` |
| Модуль | `core/services/chat/{go.mod,cmd/chat/main.go}`, запись в `core/go.work` |
| Домен и хранилище | `core/services/chat/internal/{domain,repository}/`, `migrations/000001_init.{up,down}.sql` |
| Connect | `core/services/chat/internal/server/handler.go` |
| Realtime | `core/services/chat/internal/{ws,pubsub}/` |
| Gateway | `core/services/gateway/internal/{proxy/chat.go,policy/policy.go,wsproxy/}` |
| Инфраструктура | `core/deploy/docker-compose.yml`, `docker-compose.e2e.yml`, `.env.example` |
| Web | `ui/web/src/app/(app)/chat/`, `components/chat/`, `lib/ws/useChatSocket.ts` |
| Тесты | `core/services/chat/{internal/**/*_test.go,integration,e2e}`, `ui/web/e2e/chat.spec.ts` |

Список Go-модулей продублирован в трёх местах — `GO_MODULES` в `Makefile`, args
джобы `golangci-lint` в `ci.yml`, дефолтная команда в `docker-compose.tools.yml`.
`services/chat` вписывается во все три, иначе он собирается и тестируется, но не
линтуется.

## Проверка

```bash
make proto              # контракт chat сгенерирован для Go и TS
make check              # gofmt, vet, go test -race
make lint               # golangci-lint, включая новый модуль
make test-integration   # Postgres, Redis и Kafka в testcontainers
make test-e2e           # полный стек: два клиента, доставка и реконнект
make test-web-e2e       # Playwright: два браузерных контекста в одной комнате
make web-check          # lint, typecheck, build веб-клиента
```

Ручная проверка: `make up`, два окна браузера на `http://localhost:3000`,
один аккаунт пишет — второй видит сообщение не перезагружая страницу. В логах
chat и gateway у одного сообщения один `correlation_id`.

## Критерий готовности

Два браузера обмениваются сообщениями в реальном времени, переподключение после
разрыва не теряет ни одного сообщения, и оба e2e-набора — Go и Playwright —
зелёные в CI.
