# Durak — игра со скрытой информацией

## Контекст
Durak стоит первым в списке игр `CLAUDE.md`, но его не было. Все игры до него — с открытой информацией и
детерминированными правилами. Durak первым приносит перемешанную колоду, скрытые руки и обмен
атакующий/защитник внутри одного кона. Проверяется тезис «новой игре нужен только её домен»: для Durak он
выполняется не полностью, и цель — минимальное обобщённое расширение движка плюс честный список того, что
пришлось поменять вне `games/durak`.

Результат: из каталога открывается лобби Durak; партия по ссылке или Quick match играется до конца, ни
сокет соперника, ни зритель, ни история живой партии не получают чужую руку, порядок колоды и seed.

## Правила (подкидной, только 2 игрока)
- 36 карт (6–A, 4 масти). Каждому 6 карт, следующая — козырь, лежит открытой под колодой и берётся последней.
- Первым ходит обладатель младшего козыря; если козырей ни у кого нет — место 0.
- Атакующий кладёт любую карту, защитник бьёт её старшей той же масти, любым козырем (если атака не
  козырь) или старшим козырем. Ходы строго по очереди: на одну открытую карту отвечает защитник.
- Подкидывание — карты рангов, уже лежащих на столе, не больше 6 за кон и не больше, чем было у
  защитника на начало кона.
- «Бито» (`pass` атакующего при всех покрытых картах) — стол в сброс, атакует защитник. «Беру» (`take`) —
  атакующий может ещё подкинуть в пределах лимита, затем `pass`, стол уходит защитнику, атакует тот же.
- Добор до 6 после каждого кона: сначала атакующий, затем защитник.
- Когда колода пуста, вышедший без карт выигрывает; оба без карт в одном коне — ничья.
- Жёсткий лимит ходов — общая политика `main` (#29): `Definition.MaxPlies` (`durak.MaxPlies = 1000`,
  переопределяется `GAME_DURAK_MAX_PLIES`), `State.Advance` завершает партию ничьей, `Session.move_limit`
  сообщает причину; случайная игра заканчивается за ~200 ходов.
- Нотация: `attack 7H`, `defend 7H KH`, `take`, `pass`; ранги `6789TJQKA`, масти `SCDH`, длина ≤ 12.
- Вне объёма: переводной, больше двух игроков, команды.

## Подход
- **Движок** (`games/durak`): `Position` — чистые функции `Deal(seed)`, `Legal`, `Play`, `Outcome`;
  `Game` реализует `engine.Game`. Перемешивание — Fisher–Yates на ChaCha8 из `math/rand/v2` с ключом из
  seed: алгоритм зафиксирован спецификацией, golden-тест ловит его изменение.
- **Обобщённые расширения** (опциональные интерфейсы, проверяются type assertion, `Game` не меняется):
  - `engine.Seeded { InitSeeded(players, seed) }` и поле `State.Seed` (`omitempty` — у открытых игр
    JSON не меняется). `Registry.NewState` для seeded-игры возвращает `ErrSeedRequired`,
    `Registry.NewSeededState` раздаёт по seed.
  - `engine.Viewer { View(state, viewer) }` и `Registry.View` — снимает seed у любой игры и вызывает
    проекцию, если игра её реализует. Durak прячет руку соперника (остаётся число карт), колоду
    (остаётся число, козырь открыт) и seed; зритель не видит ни одной руки; после конца партии всё открыто.
- **Seed**: `session.Rules` берёт его из `crypto/rand` в момент раздачи (`Start` или `Join`), а не из id
  игроков. Seed лежит в `State` и попадает в snapshot факта `Created`/`Joined` журнала, поэтому fold и
  `Rules.Frames`/`Replay` воспроизводят ту же раздачу.
- **Undo** для игр с `Viewer` запрещён (`session.ErrNoUndo` → `UNDO_DISABLED`): отмена хода после добора
  показала бы игроку верх колоды.
- **Граница**: `server.result` проецирует сессию для вызывающего перед рендером (`visible`), этим
  путём идут `Create/Join/Get/Leave`, `ListSessions`, `ListLiveSessions`, push по сокету (игрок и
  зритель). История живой партии проецирует каждый кадр, законченной — отдаёт всё. Легальные ходы
  считаются только для того, чей ход, и только из его видимой руки.
- **Proto** (additive, номера не пересекаются с `feat/game-checkers`: там `Session` 23,
  `SessionCommand` 8, `HistoryFrame` 8; `main` занял `Session` 22 и `HistoryFrame` 7): `Session.durak = 24`,
  `SessionCommand.durak_move = 9`, `HistoryFrame.durak = 9`, сообщения `DurakBoard`, `DurakHand`,
  `DurakPair`, `DurakMove`, enum `DurakEnd` (`DURAK`, `DRAW`; лимит ходов — общий `Session.move_limit`).
- **Рейтинг** — общий, работает без изменений. **Анализ** — только шахматы. **Турниры** — Durak не
  включается: no-show форфейт (`session.Rules.Forfeit`) считает, что место s впервые ходит на ходу s,
  а в Durak первым ходит обладатель младшего козыря, и неявившийся защитник проиграл бы только по часам.
- **Боты**: на web у каждой игры есть офлайн-практика с ботом — для Durak добавляется простой бот на
  легальных ходах (младшая некозырная карта, самая дешёвая защита, взять, если нечем бить).
- **Gateway** не меняется: он проксирует контракт как есть, `wstrace` читает команду по descriptor.
- **SwiftUI** не трогается: клиент уже скрывает неизвестные игры (#35).

## Затрагиваемые файлы
- `core/services/game/internal/games/durak/*` — домен и тесты.
- `core/services/game/internal/engine/{engine,errors,registry,state}.go` — `Seeded`, `Viewer`, `State.Seed`.
- `core/services/game/internal/session/{domain,store}.go` — seed из `crypto/rand`, `ErrNoUndo`.
- `core/services/game/internal/server/{handler,socket,spectate,durak}.go` — проекция и доска.
- `core/services/game/cmd/game/main.go` — регистрация.
- `core/shared/proto/axon/game/v1/game.proto`, затем `make proto`.
- Тесты: `journal/fold_test.go`, `session/durak_test.go`, `server/durak_test.go`,
  `integration/durak_test.go`, `e2e/durak_test.go`.
- `ui/web/src/lib/games/{catalog,durak,durak-bot}.ts`, `components/games/durak/*`,
  `app/(app)/games/durak/**`, `components/games/{catalogue,replay}.tsx`,
  `components/games/tournaments/create-dialog.tsx`, `lib/ws/use-game-socket.ts`, `e2e/durak.spec.ts`.

## Как проверяется
- `cd core && go test -race ./services/game/...` — table-driven тесты раздачи, козыря, побития,
  подкидывания, взятия/бито, добора, конца и ничьей, лимита; property-тест на 300 seed; негативные
  тесты утечек на сокет, `GetSession`, списки и историю.
- `make lint` — 0 замечаний; `make test-integration` для game (Postgres + Kafka в testcontainers).
- Go e2e через gateway на одноразовом стеке `axon-durak`: партия до конца, сокет соперника и зрителя
  ни разу не получают чужую руку.
- Web: `npx tsc --noEmit`, `npm run lint`, `npm test`, Playwright `e2e/durak.spec.ts` — два браузера
  доигрывают партию.
- Живьём на стенде `axon-durak`: создать, войти, атаковать, побить, взять, бито, доиграть, реплей;
  в кадрах WebSocket нет карт соперника.

## Критерий готовности
Двое доигрывают Durak через gateway до конца, а все проверки утечек на сокетах, `GetSession`, списках и
истории живой партии зелёные.

## Результаты
Этап закрыт: двое доигрывают Durak через gateway до конца; ни сокет соперника, ни зритель, ни `GetSession`,
списки и история живой партии не получают чужую руку, порядок колоды и seed.

### Что изменено вне `games/durak` и зачем
| Файл | Изменение | Почему без него нельзя |
|---|---|---|
| `engine/engine.go` | опциональные `Seeded`, `Viewer` | `Game` не меняется, `Init` остаётся чистым, а колоде нужен seed снаружи; проекция нужна только играм со скрытой информацией |
| `engine/state.go` | `State.Seed` (`omitempty`, строка в JSON) | seed — записанный факт: snapshot журнала, fold, compaction и replay раздают ту же колоду; открытые игры сериализуются как раньше |
| `engine/registry.go`, `errors.go` | `NewSeededState`, `Hidden`, `View`, `ErrSeedRequired` | одна точка, где seed снимается у любой игры и применяется проекция; `NewState` не даст раздать seeded-игру без seed |
| `session/domain.go`, `store.go` | seed из `crypto/rand` при `Start`/`Join`, `Replay`/`Frames` по записанному seed, `ErrNoUndo` | undo после добора показал бы верх колоды |
| `server/handler.go`, `socket.go`, `spectate.go` | проекция для вызывающего на всех границах, команда `durak_move` | без этого чужая рука уходит в сокет, `GetSession`, списки и историю |
| `server/durak.go` | `DurakBoard` из `Snapshot` | рендер доски, как у остальных игр |
| `game.proto` | `Session.durak = 24`, `SessionCommand.durak_move = 9`, `HistoryFrame.durak = 9` + сообщения | additive; номера не пересекаются с `feat/game-checkers` (23/8/8) |
| `cmd/game/main.go` | регистрация | — |
| web `catalog.ts`, `catalogue.tsx`, `replay.tsx`, `use-game-socket.ts`, `tokens.css` | каталог, реплей, `durakMove`, токены стола и карт | — |
| web `tournaments/create-dialog.tsx` | список из `tournamentGameKeys` | Durak исключён из турниров (форфейт неявки считает, что место s ходит первым на ходу s); на сервере `TOURNAMENT_GAMES` по умолчанию и так его не содержит |

Не менялись: gateway, rating (общий, проверен живьём: рейтинговый quick match двигает рейтинг), analysis
(только шахматы), SwiftUI, loadgen.

### Расхождения с замыслом
- Пока ветка жила, в `main` пришёл #29: общий лимит ходов и ограниченная история с кэшем
  (`server/history.go`, кадр несёт свой `move`, полный список ходов только в последнем кадре). После
  слияния Durak берёт лимит из `Definition.MaxPlies` вместо своего конца `move_limit`, проекция живой
  истории перенесена в `buildHistory` (кэшируются только законченные партии, им проекция не нужна),
  кадры Durak тоже несут `move`. Отключение проекции в `history()` ловит integration-тест.
- В Playwright-прогоне нашлись два дефекта вёрстки: шестая карта переносилась на вторую строку при ширине
  1024, а козырь под колодой был почти не виден. Рука теперь веером с перекрытием (сильнее при 9+ картах),
  козырь лежит ровно и выглядывает из-под колоды индексом; поднимается при наведении только играбельная карта.
- Лимит сокета (4 команды/с) срабатывает на автоматической игре: оба e2e держат паузу 300 мс на игрока.

### Проверки
- После слияния с `main` повторены unit, lint, integration, Go e2e, Playwright и живая проверка реплея; ручной проход двумя браузерами ниже сделан до слияния.
- `go test -race` по `services/game/...`, `services/gateway/...`, `shared/...` — зелёные; в пакете
  `durak` 51 тест/подтест, включая property-тест на 300 seed и fuzz нотации; `server/durak_test.go`
  — 4 негативных теста утечек (сокеты игроков и зрителя, `GetSession`, списки, история), каждый падает,
  если отключить проекцию.
- `make lint` — 0 issues (с тегами `e2e` и `integration`).
- `go test -tags integration ./services/game/integration/...` — зелёный (Kafka-журнал, проектор,
  Postgres read model, failover на второй узел).
- Go e2e на стенде `axon-durak` через gateway (`make`-аналог `test-e2e-game`): обе e2e игры зелёные,
  Durak доигран (60–73 хода, были attack/defend/take/pass).
- Web: `tsc`, `eslint` (0 ошибок), `vitest` 165/165 (из них 22 — Durak), Playwright `durak.spec.ts`
  (2 теста, 3 повтора подряд зелёные), а также `spectate`, `tic-tac-toe`, `quoridor`, `chess`,
  `tournament` на том же стенде.
- Живьём: создание, вход по ссылке вторым браузером, атака, побитие с клавиатуры, «бито»,
  «беру», доигрывание до ничьей «оба вышли», реплей (после слияния — со списком ходов из кадров), сдача,
  практика с ботом, ширина 375 px. В
  101 кадре WebSocket у второго игрока и в ответах `GetSession`/`ListLiveSessions` карт соперника,
  колоды и seed нет.
