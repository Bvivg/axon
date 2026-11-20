# Этап 4б — Quoridor

## Контекст
Quoridor — вторая «настоящая» игра после шахмат и проверка того, что после обобщения `session`/`server`
новая игра добавляется только доменом, контрактом доски и клиентом. Самое нетривиальное — стены: каждое
размещение требует BFS-проверки, что у обоих игроков остаётся путь до своей стороны.

Решения владельца (2026-11-19):
- объём как у шахмат: онлайн по ссылке и Quick match с рейтингом, офлайн-практика с ботом и вдвоём;
- только 2 игрока, по 10 стен;
- ветка `feat/game-quoridor` поверх `feat/game-chess` (PR #11).

Результат: из каталога открывается лобби Quoridor с той же кнопкой «Start a game», партия играется до
прихода фишки на противоположную сторону или до сдачи/конца времени, рейтинг меняется по Elo.

## Правила и нотация
- Доска 9×9, клетки `a1`…`i9`, клетка `r*9+c` (a1 = 0). Сиденье 0 начинает на `e1` и идёт к ряду 9,
  сиденье 1 — с `e9` к ряду 1. Белые (сиденье 0) ходят первыми.
- Ход — либо шаг фишки (`e2`), либо стена (`e3h`, `e3v`). Стена задаётся якорем `a1`…`h8` — левой
  нижней клеткой квадрата 2×2, который она делит: `h` лежит между рядами якоря и следующего и закрывает
  две клетки (якорь и правую), `v` — между столбцами якоря и следующего на два ряда.
- Пересечения: `h` на якоре конфликтует с `h` на том же якоре и соседних по столбцу и с `v` на том же
  якоре; `v` — с `v` на том же якоре и соседних по ряду и с `h` на том же якоре.
- Шаг фишки: на соседнюю клетку без стены между ними; если там соперник — прыжок через него, а если за
  ним стена или край — диагональный шаг в сторону (по стандартным правилам).
- Стена легальна, если у ставящего остались стены, она не пересекается с уже стоящими и после неё оба
  игрока ещё могут дойти до своей стороны (BFS).
- Ничьих нет; бесконечная игра обрезается часами (10 минут на сторону).

## Домен (`core/services/game/internal/games/quoridor`)
- `board.go` — клетка, стена (якорь + ориентация), парсинг/формат нотации, «есть ли стена между клетками».
- `pathfinding.go` — только BFS: `Distance(board, from, goalRow)` и `HasPath`; без знания о сессиях.
- `rules.go` — шаги фишки (с прыжками), легальность стены, `LegalMoves`.
- `quoridor.go` — `engine.Game`: состояние в `State.Data` (`pawns`, `walls`, `left`, `moves`),
  `ApplyMove` (данные хода `{"move":"e3h"}`), `View(state)` для сервера.
- Sentinel-ошибки: `ErrIllegalMove`, `ErrBadMove` (их уже понимает `moveError` через шахматный паттерн —
  добавить в switch).

## API (`core/shared/proto/axon/game/v1/game.proto`, additive)
- `Session.quoridor = 19` — `QuoridorBoard { repeated string pawns (по сиденьям), repeated string walls,
  repeated uint32 walls_left, repeated string moves, optional string last_move, repeated string
  legal_moves (шаги и стены для того, чей ход) }`.
- `SessionCommand.quoridor_move = 7` — `QuoridorMove { ply, move, optional revision }`.
- Клиент в онлайне не дублирует правила: подсветку шагов и превью стен строит по `legal_moves`.

## Сервер
- `cmd/game/main.go` — регистрация; `server/handler.go` — `board()` для Quoridor; `server/socket.go` —
  разбор `QuoridorMove` (длина ≤ 3); `moveError` — ошибки Quoridor → `ILLEGAL_MOVE`.
- `session` и gateway не меняются — это критерий архитектуры.

## Web (`ui/web`)
- `lib/games/catalog.ts` — `GameKey.Quoridor = "quoridor"`; каталог, лобби и диалог уже общие.
- `lib/games/quoridor.ts` — правила для офлайна (тот же алгоритм, что на сервере) и
  `lib/games/quoridor-bot.ts` — бот: оценка «путь соперника − мой путь», перебор шагов и стен рядом с
  кратчайшими путями, глубина 1/2/3 для Easy/Medium/Hard, в Web Worker.
- `components/games/quoridor/`:
  - `board.tsx` — сетка 9×9 с желобками; клик по клетке — шаг, наведение на желобок — превью стены,
    клик — постановка; переключатель ориентации и клавиатура;
  - `match.tsx` — онлайн: часы, рейтинги, остаток стен, сдача, итог;
  - `practice.tsx` — против бота (Easy / Medium / Hard) или вдвоём, отмена хода.
- Маршруты `app/(app)/games/quoridor/{page,[id]/page,practice/page}.tsx`.

## Затрагиваемые файлы
- `core/services/game/internal/games/quoridor/*`, `cmd/game/main.go`, `internal/server/{handler,socket}.go`.
- `core/shared/proto/axon/game/v1/game.proto`, затем `make proto`.
- `ui/web/src/{lib/games,components/games/quoridor,app/(app)/games/quoridor}/*`,
  `ui/web/src/components/games/catalogue.tsx`, `ui/web/src/lib/ws/use-game-socket.ts`.
- `ui/web/e2e/quoridor.spec.ts`.

## Как проверяется
- `go test -race ./...` в `core/services/game`: table-driven тесты на шаги (прыжок, диагональ при стене
  за соперником, края), стены (пересечение, крест, нехватка стен, полное перекрытие пути), BFS-расстояния,
  победа; `golangci-lint`, `buf lint`.
- Web: `npm run typecheck && npm run lint && npm test` (правила и бот на тех же позициях, что и Go).
- Docker e2e `ui/web/e2e/quoridor.spec.ts`: партия по ссылке с несколькими стенами до победы; Quick match
  → сдача → изменение рейтинга; практика — бот отвечает, стена ставится.
- Живьём на `https://axon.test` после пересборки game/web.

## Критерий готовности
Двое находят друг друга через Quick match в Quoridor и доигрывают партию со стенами до победы с изменением
рейтинга, практика против бота играется офлайн, а `session`/gateway не менялись.
