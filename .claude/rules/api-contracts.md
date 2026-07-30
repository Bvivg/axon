# API / Proto контракты (shared/proto)

## Источник правды
- `.proto` в `core/shared/proto` — единственное место, где определяются формы запросов/ответов. Ни один клиент (Go/Swift/TS) не пишет типы вручную под API — только генерация.
- Генерация через `buf generate` (buf.build), конфиг — в `shared/proto/buf.gen.yaml`.

## Совместимость (additive-only)
- Никогда не переиспользовать номер поля после удаления — резервировать через `reserved`.
- Новые поля — только `optional`, никогда не делать breaking rename/remove в рамках одной мажорной версии.
- Breaking change → новый пакет версии (`game.v2`), старый оставить работать, пока аналитика не покажет, что старые клиенты вымерли.

## Транспорт
- Gateway отдаёт один и тот же контракт как gRPC (внутренние сервисы, Swift-клиент), gRPC-Web и JSON (веб-клиент) через Connect — не создавать отдельный REST-контракт вручную.
- Realtime (ходы в игре, сообщения чата) — отдельный WebSocket-протокол, не пытаться просовывать в Connect/gRPC unary-запросы.

## Именование
- Сервисы: `GameService`, `ChatService`, `AuthService`, `CallingService` — по одному `.proto`-файлу на сервис в `shared/proto/<service>/v1/`.
- RPC-методы — глагол + существительное: `CreateSession`, `ApplyMove`, `ListActiveGames`.
