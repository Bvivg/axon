# Этап 2 — транспорт: `shared/pkg/kafka` и `shared/pkg/ws`

## Контекст

`roadmap.md` (этап 0) отложил эти два пакета **намеренно**: «kafka/ и ws/ в
shared/pkg не заводились — переехали на этапы 2–3, где появится первый
потребитель». Эта задача идёт против того решения по решению владельца проекта.
Риск от этого никуда не делся и формулируется точно: спроектировать удобно
воображаемому потребителю, а потом переделывать под настоящий, когда появится
`chat`.

Смягчение — не «сделать пакеты гибкими» (гибкость под неизвестного потребителя и
есть та самая ошибка), а наоборот: назвать двух конкретных первых потребителей и
писать ровно под них.

**Потребитель 1 — `chat.message`.** `chat` при приёме сообщения пишет его в
Postgres (источник истины), рассылает по репликам через Redis pub/sub и
публикует событие `chat.message` в Kafka. Читают его отдельные сервисы-потребители
(уведомления, аналитика) — каждый своей консьюмер-группой, названной по себе.
Ключ сообщения — id комнаты: порядок нужен внутри комнаты, между комнатами не нужен.

**Потребитель 2 — `game.started` / `game.finished`.** `game` публикует их из
`session` при создании и завершении партии. Ключ — id сессии. Тот же профиль
нагрузки: редкие события, at-least-once, порядок в пределах ключа.

**Потребители `ws`.** Серверная сторона — WS-эндпоинты `chat` (сообщения) и
`game` (ходы): принять апгрейд, держать соединение живым, закрыться с внятным
кодом. Клиентская сторона — Go-клиенты в e2e и в интеграционных проверках; им
нужен дозвон с backoff. Полезная нагрузка пакету неизвестна: он возит байты.

Из этих двух потребителей прямо следует, чего в пакетах **не будет**: транзакций
и exactly-once, DLQ, schema registry, батч-API, кастомных партиционеров,
встроенных ретраев с политикой, реестра подписок и хабов соединений. Каждое из
этого дописывается за час, когда появится тот, кому оно нужно; лишний слой
абстракции сейчас дороже отсутствующего.

**Результат:** два пакета в `shared/pkg`, покрытые unit-тестами на всё, что
проверяется без брокера, и integration-тестом на Kafka через testcontainers.
Ни `chat`, ни `game`, ни Kafka в dev-стеке эта задача не создаёт.

## Выбор библиотеки Kafka

`github.com/segmentio/kafka-go` — чистый Go без cgo, `Reader` даёт
консьюмер-группы с явными `FetchMessage` + `CommitMessages` (то есть ручной
коммит — ровно то, что нужно для at-least-once), `Writer` возит заголовки; ничего
сверх этого нам не требуется. (`franz-go` мощнее, но его дополнительный API
нечем оправдать перед двумя потребителями выше; `confluent-kafka-go` тянет cgo и
librdkafka, что ломает статическую сборку service-образов.)

## `shared/pkg/kafka` — интерфейс

```go
// Тема — контракт <domain>.<event> из rules/infra.md, проверяемый типом,
// а не комментарием.
type Topic string
func NewTopic(domain, event string) (Topic, error)
func ParseTopic(s string) (Topic, error)
func (t Topic) Domain() string
func (t Topic) Event() string

// Сообщение — байты плюс заголовки. Пакет не знает, что внутри Value.
type Headers map[string]string
func (h Headers) Get(key string) string           // регистронезависимо
type Message struct {
    Topic     Topic
    Key       []byte
    Value     []byte
    Headers   Headers
    Partition int       // заполняет консьюмер
    Offset    int64     // заполняет консьюмер
    Time      time.Time // заполняет консьюмер
}
func (m Message) CorrelationID() string
func (m Message) Context(ctx context.Context) (context.Context, string)

// Продюсер. Acks зашиты в RequireAll, а не вынесены в конфиг: это события
// домена, и более дешёвые режимы подтверждения обменивают на скорость ровно ту
// надёжность, ради которой шина и заводится.
type ProducerConfig struct {
    Brokers      []string
    Service      string        // имя сервиса-издателя, уходит в логи
    WriteTimeout time.Duration // 10s
    BatchTimeout time.Duration // 10ms
}
func NewProducer(cfg ProducerConfig, log *slog.Logger) (*Producer, error)
func (p *Producer) Publish(ctx context.Context, msg Message) error
func (p *Producer) Close() error

// Консьюмер.
type ConsumerConfig struct {
    Brokers []string
    Service string   // имя сервиса-ПОТРЕБИТЕЛЯ; из него берётся group id
    Topics  []Topic
    MaxWait       time.Duration // 500ms
    CommitTimeout time.Duration // 5s
}
func (c ConsumerConfig) GroupID() string
type Handler func(ctx context.Context, msg Message) error
func NewConsumer(cfg ConsumerConfig, log *slog.Logger) (*Consumer, error)
func (c *Consumer) Run(ctx context.Context, h Handler) error
func (c *Consumer) Close() error

// И продюсер, и консьюмер реализуют health.Checker (Name/Check через
// metadata-запрос) — тем же способом, что postgres.Pool и redis.Client:
// rules/observability.md требует, чтобы /readyz проверял зависимости, и Kafka
// названа там прямо.
```

Решения, которые стоит зафиксировать, потому что они не самоочевидны:

- **Группа = имя сервиса-потребителя, а не свободная строка.** `rules/infra.md`
  запрещает делить одну группу между сервисами; поле называется `Service`, и
  `GroupID()` выводится из него — правило нельзя нарушить опечаткой в конфиге.
- **correlation_id: у продюсера `Ensure`, у консьюмера `WithID` + `Ensure`.**
  Продюсер без ID в контексте сгенерировал бы разрыв цепочки, поэтому он его
  создаёт и тем же id логирует собственную публикацию. Консьюмер поднимает
  заголовок в контекст, а при его отсутствии генерирует новый: для этого
  сообщения консьюмер — граница системы, ровно как gateway для HTTP-запроса.
- **Ошибка хендлера останавливает `Run` и возвращается наружу, офсет не
  коммитится.** Альтернатива — залогировать и продолжить — теряет сообщение
  молча: следующий успешный коммит перепрыгнет незакоммиченный офсет. Ретраи,
  backoff и DLQ — политика конкретного потребителя, транспорт её не выбирает.
- **`BatchTimeout` выставляется явно (10 мс).** Дефолт `kafka-go` — 1 секунда, и
  одиночное сообщение чата ждало бы её целиком. Это тот случай, когда
  «не полагаться на дефолты» стоит latency чата.
- **Отмена контекста — не ошибка.** `Run` при `ctx.Done()` возвращает `nil`:
  штатная остановка сервиса не должна выглядеть в логах как сбой.
- **`LoadConfig` из env не пишем.** Он потребует `KAFKA_BROKERS` в
  `core/deploy/.env.example`, а этот файл принадлежит параллельной сессии;
  переменную заводит `chat` вместе с брокером в compose.

## `shared/pkg/ws` — интерфейс

```go
type Options struct {   // общие для сервера и клиента, все дефолты явные
    PingInterval, PingTimeout time.Duration // 30s / 10s, отрицательное — выключить
    WriteTimeout time.Duration              // 10s
    ReadLimit    int64                      // 32 KiB
    MessageType  MessageType                // text по умолчанию: браузер отдаёт
                                            // такой фрейм в JS строкой
    Logger *slog.Logger
}
type AcceptOptions struct { Options; Subprotocols, OriginPatterns []string }
type DialOptions   struct { Options; Subprotocols []string; Header http.Header;
                            HTTPClient *http.Client; HandshakeTimeout time.Duration }

func Accept(w http.ResponseWriter, r *http.Request, opts AcceptOptions) (*Conn, error)
func Dial(ctx context.Context, url string, opts DialOptions) (*Conn, error)
func DialRetry(ctx context.Context, url string, opts DialOptions, b Backoff) (*Conn, error)

func (c *Conn) Read(ctx context.Context) ([]byte, error)
func (c *Conn) Write(ctx context.Context, data []byte) error
func (c *Conn) Close(code StatusCode, reason string) error
func (c *Conn) CloseNow() error
func (c *Conn) CorrelationID() string
func (c *Conn) Subprotocol() string

func CloseCodeFor(err error) (StatusCode, string)  // ошибка → корректный код закрытия
func TruncateReason(reason string) string          // до MaxCloseReason = 123 байт

type Backoff struct { Initial, Max time.Duration; Factor, Jitter float64 }
func (b Backoff) Delay(attempt int) time.Duration
func (b Backoff) Validate() error
var DefaultBackoff = Backoff{Initial: 500ms, Max: 30s, Factor: 2, Jitter: 0.2}
```

Решения:

- **Keepalive требует, чтобы кто-то звал `Read`.** Это ограничение
  `coder/websocket`: `Ping` ждёт pong, а читает его read-цикл. Оба наших
  потребителя и так читают. Ограничение фиксируется в doc-комментарии, а не
  обходится собственным read-циклом с каналами — тот потребовал бы решений про
  буферизацию и backpressure, которых сейчас никто не заказывал.
- **`reason` в close-фрейме обрезается до 123 байт, по границе руны.**
  Библиотека отказывается отправлять слишком длинный reason, то есть
  динамический текст ошибки приводит не к некрасивому закрытию, а к отсутствию
  close-фрейма вовсе; а обрезка посреди руны дала бы невалидный UTF-8, который
  в close-фрейме запрещён.
- **`CloseTimeout` в `Options` не появился, хотя был в первом наброске.**
  `coder/websocket` держит таймаут close-хендшейка зашитым (5 с) и не даёт его
  настроить — поле было бы обещанием, которого пакет не может выполнить.
- **`CloseCodeFor` переводит ошибку в код:** превышение лимита чтения →
  `StatusMessageTooBig`, отмена/дедлайн → `StatusGoingAway`, остальное →
  `StatusInternalError`. Чистая функция, проверяется unit-тестом.
- **`OriginPatterns` обязателен на сервере и не имеет дефолта `*`** —
  `rules/security.md` про явный allow-list. Пустой список означает
  «только same-origin», как в самой библиотеке.
- **correlation_id проходит и через WS-хендшейк:** `Accept` читает
  `X-Correlation-Id` из апгрейд-запроса, `Dial` кладёт его из контекста в
  заголовок. Без этого лог WS-сессии не связывается с запросом, который её открыл.

## Затрагиваемые файлы

| Файл | Что |
|---|---|
| `core/shared/pkg/kafka/{topic,message,producer,consumer}.go` | пакет |
| `core/shared/pkg/kafka/{topic,message,config}_test.go` | unit |
| `core/shared/pkg/ws/{conn,accept,dial,backoff,close}.go` | пакет |
| `core/shared/pkg/ws/{backoff,close,conn}_test.go` | unit |
| `core/shared/integration/{main_test.go,kafka_test.go}` | integration за тегом `integration` |
| `core/shared/go.mod`, `go.sum` | kafka-go, coder/websocket, testcontainers |
| `Makefile` | одна строка: путь в цели `test-integration` |

Интеграционные тесты живут в модуле `shared`, а не в отдельном модуле: отдельный
потребовал бы записи в `core/go.work`, который принадлежит параллельной сессии.
Из-за этого testcontainers попадает в `shared/go.mod` — на сборку сервисов это не
влияет, module graph pruning (Go 1.17+) не тащит тестовые зависимости
импортируемого модуля в зависимый.

## Как проверяется

- `make check` — `gofmt`, `go vet`, `go test -race` по всему workspace. Ожидание:
  новые пакеты собираются и их unit-тесты зелёные.
- `make lint` — `golangci-lint`, включая файлы за тегом `integration`.
- `make test-integration` — testcontainers поднимает Postgres (auth) и Kafka
  (shared). Ожидание: обе группы зелёные; брокер тушится после прогона.
  Kafka-часть идёт около 2,5 минут: почти всё время — это join и rebalance
  консьюмер-групп, по группе на сценарий. `ws` своей integration-цели не имеет
  намеренно: соединение поднимается в процессе через `httptest`, и брокер или
  контейнер там нечего проверять.
- `make up` / `make test-e2e` не запускаются: dev-стек занят параллельной сессией,
  и ни один сервис пока эти пакеты не импортирует, так что e2e ничего бы не
  показал.

Что покрыто unit-тестами (без брокера и без сети наружу): разбор и валидация
темы `<domain>.<event>`, регистронезависимый разбор заголовков, перенос
correlation_id сообщение ↔ контекст, вывод group id из имени сервиса, валидация
конфигов; в `ws` — арифметика backoff (рост, потолок, границы джиттера),
соответствие ошибки коду закрытия, обрезка reason, а также полный
round-trip через `httptest`: эхо, keepalive-пинги, graceful close с доставкой
кода пиру, превышение read limit, `DialRetry` после нескольких отказов.

Что покрыто integration-тестом (реальный брокер): publish → consume, ключ и
заголовки доезжают, correlation_id доезжает и восстанавливается в контексте,
отсутствие заголовка даёт новый id, ручной коммит (перезапуск консьюмера той же
группой не выдаёт заново то, что закоммичено), ошибка хендлера оставляет
сообщение незакоммиченным и оно приходит снова, graceful close продюсера и
консьюмера.

## Критерий готовности

`make check`, `make lint` и `make test-integration` зелёные на ветке, а первый
реальный потребитель (chat на этапе 2) может опубликовать `chat.message` и
прочитать её своей группой, не дописывая ничего в `shared/pkg/kafka`.
