# Next.js (ui/web)

## Структура
- `app/` — маршруты, `components/` — переиспользуемые UI-компоненты, `lib/` — клиенты/хуки без JSX.
- Игровые компоненты — в `components/games/<game>/`, доменная логика хода (если есть client-side hint) отдельно от рендеринга (`Board.tsx` рендерит, `useQuoridorSocket.ts` — только транспорт).

## Данные
- RPC — только через `@connectrpc/connect-web` (сгенерированные клиенты из `shared/proto`), никаких ручных `fetch` к gateway.
- Realtime — `react-use-websocket`, переподключение/backoff настраивать явно, не полагаться на дефолты.
- Server state — через TanStack Query; local UI state — обычный `useState`/`useReducer`, не смешивать с query-кэшем.
- `axios` — только как fallback для не-Connect эндпоинтов (если такие появятся), не как основной способ похода к gateway.

## UI
- shadcn + @base-ui — базовые примитивы, Tailwind — только utility-классы, без кастомного CSS поверх, если не оговорено отдельно (см. frontend-design skill при генерации).
- Виртуализация списков (лидерборды, история игр) — TanStack Virtualizer, не рендерить сотни строк напрямую.

## Типизация
- Типы данных — только сгенерированные из proto, shared-types не редактируются вручную.
