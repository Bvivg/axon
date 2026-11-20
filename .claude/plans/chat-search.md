# Chat — поиск по сообщениям чата (срез 6 переноса из nod)

Общий план: `chat-port-from-nod.md`.

## Контекст

Найти старое сообщение можно только прокруткой; в длинном треде (тысячи сообщений)
это минуты. В nod в шапке треда есть поиск: подстрока без учёта регистра, последние
совпадения сверху, клик переносит к сообщению. Переносим.

Результат: в шапке чата кнопка поиска, ввод от двух символов показывает до 50
совпадений (новые сверху), клик открывает ленту на этом сообщении, даже если оно
на десятки страниц выше загруженного.

## Решения

### Бэкенд
- Поиск — `ILIKE '%…%'` по `messages.body` (подстрока, как в nod; полнотекстовый
  поиск со стеммингом не нужен — ищут имена, слова из середины, фрагменты ссылок).
  `%`, `_` и `\` в запросе экранируются.
- Индекс — GIN `gin_trgm_ops` по `body` (частичный: `deleted_at IS NULL AND kind <> 'system'`).
  `CREATE EXTENSION` требует суперпользователя, а миграции chat идут от
  `chat_service`, поэтому расширение создаёт init-скрипт Postgres
  `core/deploy/postgres/init/02-extensions.sql` (в схеме `public`), а миграция
  `000010_message_search` ссылается на `public.gin_trgm_ops`. На уже
  инициализированной базе init не запускается — `make db-extensions` (`up -d --wait postgres` + тот
  же файл), `migrate-up` вызывает его перед миграциями.
- RPC `SearchMessages(room_id, query)` → `messages` (≤50, `seq` по убыванию).
  Запрос — 2..100 символов после trim, иначе `invalid_argument`. Только участник
  (`not_found` для чужих), без удалённых и системных, без очищенной части истории
  (`cleared_through_seq`) — те же границы, что у `ListMessages`.
- Gateway: policy (дешёвое чтение) + proxy.

### Веб
- Кнопка поиска в шапке чата: заголовок сменяется полем ввода, под шапкой —
  панель результатов (`ScrollArea`, `max-h-72`). Esc или крестик закрывают.
- Запрос уходит после паузы 300 мс (TanStack Query, ключ — комната + запрос).
- Строка результата: автор, время, превью с подсвеченным совпадением.
- Клик — закрыть поиск и перейти к сообщению механизмом из среза 1 (`onJumpTo`).
  Если сообщение далеко выше загруженного, `loadHistoryThrough` сначала докачивает
  историю страницами по 200 (максимум сервера) прямо в кэш ленты, пока подгрузка
  при прокрутке приостановлена; 429 повторяется раз в секунду. Тред 3000 → ~3 с.
- Шапка с полем поиска той же высоты, что без него (`h-10`), панель результатов
  накрывает ленту, не сдвигая её.
- Подсветка — чистая функция `snippetOf` (`lib/chat/search.ts`): совпадения
  регуляркой `giu` по экранированному запросу, длинное тело начинается с границы
  слова за 24 символа до первого совпадения.

### Догрузка пропущенного при подписке (найдено e2e поиска)
- WS `subscribe` с `since` догонял одной страницей по умолчанию (50) и молча
  обрезал остальное: после >50 сообщений, пришедших, пока чат не открыт, в ленте
  оставалась дыра (кэш истории `staleTime: Infinity` не перечитывается).
- Теперь догрузка — до 200 сообщений; если пропущено больше, `subscribed` приходит
  с `behind: true` без частичной догрузки, а клиент сбрасывает доставленное и
  перечитывает свежую страницу (`refreshNewestPage`), старое — прокруткой.

## Файлы
- `core/deploy/postgres/init/02-extensions.sql`, `Makefile` (`db-extensions`)
- `core/services/chat/migrations/000010_message_search.{up,down}.sql`
- `core/shared/proto/axon/chat/v1/chat.proto`
- `core/services/chat/internal/{domain/validation.go,repository/message.go,service/service.go,server/handler.go}`
- `core/services/chat/internal/ws/{handler.go,protocol.go}` — `behind` при подписке
- `core/services/gateway/internal/{policy/policy.go,proxy/chat.go}`
- `ui/web/src/lib/query/chat.ts`, `ui/web/src/components/chat/{message-search.tsx,chat-room-view.tsx}`
- `ui/web/src/lib/chat/search.ts`, `ui/web/src/lib/ws/{protocol.ts,use-chat-socket.ts}`
- Тесты: `domain/validation_test.go`, `repository/search_test.go`, `service/search_test.go`,
  `ws/ws_test.go`, `integration/search_test.go`, `e2e/search_test.go`,
  `ui/web/src/lib/chat/search.test.ts`, `ui/web/e2e/search.spec.ts`

## Проверка
- Unit: `ValidateSearchQuery` (границы длины, trim), экранирование шаблона,
  `snippetOf` (vitest), подписка с отставанием >200 → `behind` (ws).
- Integration (`go test -tags integration ./services/chat/integration/...`):
  регистр, `%`/`_` буквально, удалённые/системные не находятся, очищенная история
  не находится, лимит 50 и порядок.
- Core e2e (`make test-e2e`): поиск через gateway, чужой чат → `not_found`,
  короткий запрос → `invalid_argument`.
- Web e2e (`search.spec.ts`): 250 сообщений через WS, поиск слова из самого начала
  → клик → сообщение в зоне видимости.
- Живьём: тред Ada↔Bob (3000+), поиск по слову из начала, переход.

## Критерий готовности
Поиск в шапке чата находит сообщение по подстроке и переносит к нему в ленте;
тесты зелёные.
