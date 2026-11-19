# Этап 4 — шахматы

## Контекст
Шахматы — флагманская игра Axon и проверка архитектуры движка: новая игра реализует тот же
интерфейс `Game`, а матчмейкинг, рейтинг, часы, сдача и выход достаются ей бесплатно. Решения
владельца (2026-11-19):
- онлайн по ссылке и Quick match с рейтингом, плюс офлайн-практика с ботом и вдвоём на одном
  устройстве;
- полные правила FIDE, включая ничью по соглашению;
- свой минималистичный набор фигур в SVG, без сторонних ассетов;
- ветка `feat/game-chess` поверх PR #10 (матчмейкинг).

Результат: из каталога игр открывается лобби шахмат с той же кнопкой «Start a game», партия
играется до мата, пата, ничьей или сдачи, рейтинг меняется по Elo.

## Домен (`core/services/game/internal/games/chess`)
- `board.go` — доска 8×8 (`[64]piece`), FEN туда и обратно, права рокировки, поле взятия на
  проходе, счётчики полуходов и ходов.
- `movegen.go` — псевдолегальные ходы, атакованность поля, легальность через «сделать ход и
  проверить своего короля». Рокировка: король не под шахом, не проходит и не встаёт на битое
  поле, между ним и ладьёй пусто.
- `san.go` — запись ходов в SAN (`Nf3`, `exd6 e.p.` пишется как `exd6`, `O-O`, `e8=Q+`, `Qxf7#`)
  с разрешением неоднозначности.
- `rules.go` — окончание партии: мат, пат, недостаток материала (K–K, K+B–K, K+N–K,
  K+B–K+B с однопольными слонами), троекратное повторение позиции (ключ — расстановка,
  очередь хода, права рокировки, поле взятия на проходе), правило 50 ходов. Ничья по
  повторению и 50 ходам засчитывается автоматически: в онлайне некому «требовать» ничью, и
  это стандартная практика онлайн-платформ.
- `chess.go` — `engine.Game`: `Init`, `ApplyMove` (ход в UCI: `e2e4`, `e7e8q`), `ValidMoves`,
  `IsTerminal`. Состояние в `State.Data`: `fen`, `moves` (UCI), `san`, `positions` (ключи для
  повторения), `end` (причина окончания).
- Sentinel-ошибки: `ErrIllegalMove`, `ErrBadMove`.

## Сессия и сервер (разовое обобщение)
До шахмат `session.Store.Move` и WebSocket принимали только клетку крестиков-ноликов. Один раз
делаю их общими, чтобы следующая игра правок в `session`/`server` уже не требовала:
- `Store.Move(ctx, id, user, ply, game engine.Key, data json.RawMessage, revision)` — сервер
  передаёт данные хода движку как есть; несоответствие игры сессии — `engine.ErrGameMismatch`.
- Ничья по соглашению — уровень сессии, не движка: колонки `game.sessions.draw_offered_by uuid`
  и `agreed_draw boolean` (миграция `000004_draw_offers`). Предложение снимается любым ходом;
  принятие завершает партию ничьей (`State.Finish(NoSeat)`), рейтинг начисляется как за ничью.
- Время: 10 минут на партию для шахмат, 5 — для крестиков-ноликов (`Definition.TimeLimit`).

## API (`core/shared/proto/axon/game/v1/game.proto`, additive)
- `Session.chess = 16` — `ChessBoard { fen, repeated san, optional last_move, bool check,
  repeated legal_moves (UCI для того, чей ход), optional ChessEnd end }`.
- `Session.draw_offer_user_id = 17`, `Session.agreed_draw = 18`.
- `ChessEnd`: `CHECKMATE`, `STALEMATE`, `INSUFFICIENT_MATERIAL`, `THREEFOLD_REPETITION`,
  `FIFTY_MOVES`.
- `SessionCommand.chess_move = 5` — `ChessMove { ply, uci, optional revision }`;
  `SessionCommand.draw = 6` — `DrawCommand { DrawAction action (OFFER, ACCEPT, DECLINE),
  optional revision }`.
- `SessionError.ILLEGAL_MOVE = 9`.
- Клиент не дублирует правила в онлайне: подсветку ходов строит по `legal_moves` с сервера.

## Web (`ui/web`)
- Маршруты `app/(app)/games/chess/{page,[id]/page,practice/page}.tsx`; каталог получает пункт
  Chess; `gameHref` и лобби параметризуются ключом игры (`tictactoe` | `chess`), диалог
  «Start a game» общий.
- `components/games/chess/`:
  - `pieces.tsx` — собственные плоские SVG-фигуры (силуэты из простых фигур, цвета из токенов);
  - `board.tsx` — доска 8×8, выбор фигуры кликом и перетаскиванием (pointer events),
    подсветка ходов и последнего хода, шах, поворот доски за чёрных, окно выбора фигуры при
    превращении, клавиатура (стрелки + Enter);
  - `moves.tsx` — история в SAN;
  - `match.tsx` — онлайн-партия: часы (`ArenaPlayer`), рейтинги, предложить/принять/отклонить
    ничью, сдача, итог с причиной и изменением рейтинга;
  - `practice.tsx` — офлайн: против бота (Easy / Medium / Hard) или вдвоём, своя сторона,
    отмена хода.
- Правила офлайн — `chess.js` (BSD-2), бот — свой alpha-beta с оценкой по материалу и
  таблицам полей (`lib/games/chess-bot.ts`), считается в Web Worker, чтобы не подвешивать UI.

## Затрагиваемые файлы
- `core/services/game/internal/games/chess/*`, `cmd/game/main.go` (регистрация),
  `internal/engine/registry.go` (`TimeLimit`), `internal/session/{store,clock,draw}.go`,
  `internal/server/{handler,socket}.go`, `migrations/000004_draw_offers.{up,down}.sql`.
- `core/shared/proto/axon/game/v1/game.proto`, затем `make proto`.
- `ui/web/src/components/games/{catalogue.tsx,chess/*}`, `ui/web/src/components/games/tictactoe/{lobby,start-dialog}.tsx`
  (обобщение по игре), `ui/web/src/lib/{query/games.ts,ws/use-game-socket.ts,games/chess-bot.ts}`.
- `ui/web/e2e/chess.spec.ts`.

## Как проверяется
- `go test -race ./...` в `core/services/game`:
  - perft из стартовой позиции (глубина 1–4: 20, 400, 8902, 197281) и позиций Kiwipete и
    «position 3/4/5» с chessprogramming.org — ловят ошибки рокировки, взятия на проходе и
    превращения;
  - table-driven `ApplyMove` из FEN: мат, пат, каждая причина ничьей, нелегальные ходы
    (связанная фигура, рокировка через битое поле, ход под шах), SAN;
  - integration на Postgres: предложение и принятие ничьей с начислением рейтинга, снятие
    предложения ходом.
- `golangci-lint`, `buf lint`; web — `npm run typecheck && npm run lint && npm test` (бот и
  обёртки).
- Docker e2e `ui/web/e2e/chess.spec.ts`: два игрока по ссылке играют «детский мат» →
  `Checkmate`, рейтинги не меняются; Quick match в шахматы → ничья по соглашению → `±0` у
  равных рейтингов; практика: бот отвечает на ход, превращение пешки через выбор фигуры.
- Живая проверка на `https://axon.test` после пересборки game, gateway и web и миграции 000004.

## Критерий готовности
Двое находят друг друга через Quick match в шахматы и доигрывают партию до мата или ничьей с
изменением рейтинга, а практика против бота играется офлайн.

## Расхождения с замыслом (по ходу работы)
- Время на сторону хранится в `engine.Definition.TimeLimit` (шахматы 10 минут), сессия берёт его через
  `Store.NewClock`/`TimeLimitMS`.
- Общий загрузчик партии вынесен в `components/games/session-loader.tsx`, лобби и диалог «Start a game» —
  в `components/games/{lobby,start-dialog}.tsx` с параметром игры; ключи игр — `lib/games/catalog.ts`.
- `ArenaPlayer` принимает свой значок и подпись стороны, чтобы шахматы переиспользовали карточку игрока
  крестиков-ноликов.
- Практика идёт без часов: в офлайне часы против бота только мешают.
- Найдено тестами: при превращении пешки не сбрасывался счётчик 50 ходов (тип фигуры проверялся после
  замены пешки) — исправлено.
- Найдено при быстрой автоигре: лимит команд сокета (4/с, запас 8) закрывал соединение с кодом 1008, а клиент
  на 1008 не переподключается, и партия зависала. Теперь лишняя команда отклоняется ошибкой
  `SESSION_ERROR_RATE_LIMITED = 11` без разрыва, а 1008 остаётся только для флуда (20/с, запас 40).
