# Chat — фото, видео, файлы и голосовые

## Контекст

Бэкенд чата уже знает виды `voice` и `attachment` и умеет класть файл в MinIO
(`chat-attachments`, `POST /api/attachment` через gateway), но веб отправляет
только текст. Пользователь попросил следующий виток:

- фото и видео как отдельные сообщения-медиа: слегка сжатые, без метаданных
  (EXIF, GPS, модель камеры);
- файлы — без потерь, как есть;
- голосовые сообщения;
- медиа чата отдельно — общий список фото/видео, файлов и голосовых комнаты;
- всё хранится в MinIO.

Сейчас payload вложения присылает клиент (`url`, `mime`, `size_bytes`), и
сервис ему верит: в чужой чат можно подсунуть любой внешний URL (трекинг-
пиксель, раскрывающий IP собеседника) и любые размеры. Медиа с серверной
обработкой это закрывает заодно: payload собирает сервер.

Результат: в вебе можно отправить фото или видео (сжатые, без метаданных),
файл (байт-в-байт), голосовое; всё рисуется в переписке, открывается на весь
экран и собрано в «Media» комнаты.

## Решения

### Два новых вида: `image` и `video`

`messages.kind` расширяется до `text | voice | attachment | image | video |
system` (новая миграция меняет `CHECK`). `attachment` остаётся «файлом как
есть», `image`/`video` — обработанные медиа. В proto — `MESSAGE_KIND_IMAGE = 5`,
`MESSAGE_KIND_VIDEO = 6` и новые ветки `oneof payload`:

```proto
message ImagePayload { string url = 1; string thumbnail_url = 2; int32 width = 3; int32 height = 4; int64 size_bytes = 5; string mime = 6; }
message VideoPayload { string url = 1; string poster_url = 2; int32 width = 3; int32 height = 4; int64 duration_ms = 5; int64 size_bytes = 6; string mime = 7; }
```

`VoicePayload` получает `optional string mime` и `optional int64 size_bytes`.
Подпись к медиа — существующее `body`.

### Загрузка создаёт запись, отправка ссылается на неё

`POST /api/attachment?as=media|file|voice` → chat обрабатывает, кладёт в MinIO и
пишет строку в новую таблицу `chat.uploads (id, uploader_id, kind, payload,
created_at)`, отвечая `{upload_id, kind, payload}`. WS-кадр `send` получает
`upload_id`; сервис берёт вид и payload из записи, только если её загрузил сам
отправитель. Клиентский `payload` для `voice`/`attachment`/`image`/`video`
больше не принимается (`system` и так закрыт). Живых клиентов, шлющих payload
напрямую, нет — веб слал только текст.

Незаконченные загрузки (загрузил, не отправил) пока не чистятся — отдельная
задача с TTL, записана здесь, чтобы не потерялась.

### Обработка — в chat, синхронно, с ограничением параллельности

Пакет `internal/media`:

- **Фото** (JPEG/PNG/WebP/GIF, определяется по байтам, не по заголовку
  клиента): `imaging` с `AutoOrientation` (как у аватарок), длинная сторона до
  2560 px, JPEG q85; PNG с прозрачностью остаётся PNG. Перекодирование само
  выбрасывает EXIF/XMP/ICC-комментарии. Превью 480 px, JPEG q75. GIF
  перепаковывается кадр-в-кадр (`gif.DecodeAll`/`EncodeAll`) — анимация
  сохраняется, расширения-комментарии нет.
- **Видео**: `ffmpeg` → H.264 (CRF 23, `veryfast`, длинная сторона до 1920,
  `yuv420p`) + AAC 128k, `+faststart`, `-map_metadata -1 -map_chapters -1`;
  постер — кадр на 0.5 с, 480 px; размеры и длительность — из `ffprobe`
  результата, не от клиента.
- **Голосовое**: что бы ни записал браузер (`audio/webm;codecs=opus` в
  Chromium, `audio/mp4` в Safari) → AAC mono 48k в `.m4a`: играет везде,
  включая будущий SwiftUI-клиент (AVFoundation не играет WebM/Opus).
  Длительность — `ffprobe`.
- **Файл**: байт-в-байт. `Content-Type` — по сниффингу, плюс
  `Content-Disposition: attachment` с исходным именем, чтобы HTML/SVG из
  публичного бакета не открывались inline.

Загрузка пишется во временный файл, не в память; ffmpeg запускается через
`exec.CommandContext` с таймаутом, одновременно — не больше `MEDIA_WORKERS`
(по умолчанию 2) задач. Лимиты: фото 25 МБ, голосовое 10 МБ, видео и файл
100 МБ (gateway-прокси поднимается до 100 МБ).

Асинхронная очередь (Kafka-джоба, статус «обрабатывается») отвергнута для
этого шага: сообщение отправляется только после готовой загрузки, так что
синхронный ответ проще и не требует промежуточных состояний в UI.

### ffmpeg в образе chat

`Dockerfile.service` получает `ARG RUNTIME_FLAVOR` (`static` | `media`):
`runtime` для `media` — alpine + `ffmpeg` под non-root пользователем, для
остальных сервисов — прежний distroless. Dev-таргет ставит `ffmpeg` при том же
флаге. Compose-файлы и CI-матрица передают `media` только для chat.

### Медиа комнаты — фильтр по видам в `ListMessages`

`ListMessagesRequest` получает `repeated MessageKind kinds` (пусто — без
фильтра). Веб показывает «Media» из шапки чата: вкладки Media (сетка превью),
Files, Voice, с подгрузкой по курсору.

### Веб

- Composer вынесен из `chat-room-view.tsx`: скрепка → «Photo or video» / «File»,
  вставка и drag&drop картинок, чипы загрузок с прогрессом и отменой, подпись
  из поля ввода; при пустом поле вместо Send — микрофон (запись, таймер,
  отмена, отправка).
- Загрузка — `axios` с `onUploadProgress` (эндпоинт не Connect —
  `rules/nextjs-web.md`).
- Переписка рисует по виду: фото с зарезервированным соотношением сторон,
  видео с постером, карточка файла (имя, размер, скачать), плеер голосового
  (play/pause, прогресс, длительность); фото и видео открываются на весь экран.
- Превью последнего сообщения в списке чатов: «Photo», «Video»,
  «Voice message», имя файла.

### По ходу работы

- Redis fan-out терял `kind`, `payload`, `reply_to_id`, `forwarded_from_id`
  (старая ошибка, до медиа не проявлялась: все живые сообщения были текстом) —
  провод pubsub теперь несёт их, покрыто integration-тестом.
- CORS gateway не пропускал `X-Filename` — загрузка из браузера падала на
  preflight; добавлен в разрешённые заголовки.
- Событие Kafka `chat.message` получило `kind`.

## Файлы

| Что | Где |
|---|---|
| Миграция | `core/services/chat/migrations/000004_media_messages.{up,down}.sql` — `CHECK` на `kind`, `uploads` |
| Контракт | `core/shared/proto/axon/chat/v1/chat.proto` — виды, `ImagePayload`, `VideoPayload`, поля `VoicePayload`, `kinds` в `ListMessagesRequest` |
| Домен | `core/services/chat/internal/domain/` — `MessageKindImage/Video`, `Upload`, payload-типы, валидация без клиентского payload |
| Обработка | `core/services/chat/internal/media/` — `image.go` (фото/GIF/превью), `ffmpeg.go` (видео, голос, белые списки демуксеров, семафор) |
| Хранилище | `core/services/chat/internal/attachment/` — `Put` с `Content-Disposition`, `pipeline.go` (media/file/voice → объекты MinIO) |
| Загрузка | `core/services/chat/internal/server/attachment.go` — `as=…`, временный файл, запись `uploads` |
| Репозиторий/сервис | `core/services/chat/internal/repository/`, `internal/service/service.go` — `uploads`, отправка по `upload_id`, фильтр `kinds` |
| WS | `core/services/chat/internal/ws/protocol.go`, `handler.go` — `upload_id`, отказ клиентскому payload |
| Fan-out | `core/services/chat/internal/pubsub/redis.go`, `internal/events/events.go` |
| Gateway | `core/services/gateway/internal/attachmentproxy/` — лимит 100 МБ, `as`, отдельный клиент с таймаутом 4 мин, 60 загрузок/мин; `internal/cors/` — `X-Filename` |
| Образ | `core/deploy/Dockerfile.service`, `docker-compose*.yml`, `.github/workflows/ci.yml` |
| Web | `ui/web/src/components/chat/` — `message-composer.tsx`, `message-content.tsx`, `media-viewer.tsx`, `room-media.tsx`, `transcript.tsx`, `chat-room-view.tsx` (drag&drop, «Media»); `ui/web/src/lib/chat/` — `upload.ts`, `use-pending-uploads.ts`, `use-voice-recorder.ts`, `content.ts`; `lib/ws/`, `lib/query/chat.ts` |
| E2E | `core/services/chat/e2e/media_test.go` + `testdata/`; `ui/web/e2e/media.spec.ts`, `chat-support.ts`, `fixtures/clip.mov`; фейковый микрофон в `playwright.config.ts` |

## Как проверяется

- `make check`, `make lint` — unit на `internal/media`: фото с EXIF-ориентацией 6
  и GPS выходит повёрнутым и без APP1; видео/голос — тесты пропускаются без
  `ffmpeg` на хосте и гоняются в контейнере chat.
- `make test-integration` — `uploads` и фильтр `kinds` на реальном Postgres.
- `make test-e2e` — загрузка фото/видео/файла/голоса через gateway, отправка по
  `upload_id`, чужой `upload_id` и клиентский payload отклоняются.
- `make test-web-e2e` — `media.spec.ts`: фото, видео (фикстура), файл (байты
  совпадают со скачанным), голосовое через фейковый микрофон Chromium; вкладки
  «Media». Страница в e2e открыта по `http://web.axon.test:3000` — не secure
  context, поэтому Chromium запускается с `--unsafely-treat-insecure-origin-as-secure`
  и `channel: "chromium"` (headless shell этот флаг игнорирует).
- Руками в браузере на дев-стенде: фото с телефона без GPS в скачанном файле
  (`exiftool`/`ffprobe` в контейнере), видео играет, голосовое записывается и
  играет.

## Критерий готовности

В вебе отправляются фото и видео (сжатые, без метаданных), файлы (байт-в-байт)
и голосовые, всё видно в переписке и в «Media» комнаты, payload берётся только
из серверной загрузки, и все наборы тестов зелёные.
