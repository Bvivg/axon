# Матчмейкинг и рейтинг в играх

## Контекст
Сейчас партию можно только создать и отправить ссылку другу. Рейтинга в бэкенде нет: в web
показаны выдуманные `nomad_01 1488` / `ak_mergen 228`. Нужен поиск соперника: игрок встаёт в
очередь, система подбирает ближайшего по рейтингу. Если за отведённое время близкого нет —
берёт любого из очереди. Время ожидания растёт вместе с числом активных игроков. Рейтинг
хранится по каждой игре отдельно и меняется после рейтинговой партии, у каждой партии есть
история изменения рейтинга.

Результат: из лобби одной кнопкой можно найти соперника, сыграть, и рейтинг обоих
изменится. Приватная партия по ссылке остаётся.

## UX
- В лобби Tic-Tac-Toe одна главная кнопка **«Start a game»**. Под ней — история партий с
  результатом и изменением рейтинга (`+12`, `−8`).
- По кнопке открывается диалог с тремя вариантами:
  - **Quick match** — рейтинговая партия, поиск соперника;
  - **Private game** — партия по ссылке, без рейтинга;
  - **Practice offline** — против компьютера или вдвоём на одном устройстве.
- **Поиск.** В диалоге таймер ожидания, текущий допуск по рейтингу (`±150`) и «Cancel».
  Когда соперник найден, сразу переход в партию.
- **Рейтинг на экране.** В партии и в колонке игр показывается настоящий рейтинг. Выдуманные
  ник и рейтинг в офлайн-практике убираются.

## Модель данных (`core/services/game/migrations/000003_ratings_matchmaking`)
- `game.ratings` — текущий рейтинг игрока в конкретной игре:
  - колонки `user_id uuid`, `game text`, `rating integer NOT NULL DEFAULT 1200`, `games`,
    `wins`, `losses`, `draws`, `created_at`, `updated_at`;
  - первичный ключ `(user_id, game)`;
  - тип игры — `text` со значением из реестра движка (`tictactoe`, позже `chess`). В Go это
    `engine.Key`, enum в БД не нужен: список игр живёт в `registry.go`.
- `game.rating_changes` — история изменения рейтинга:
  - колонки `session_id`, `user_id`, `game`, `before`, `after`, `created_at`;
  - первичный ключ `(session_id, user_id)` — повторное начисление за одну партию невозможно;
  - `FOREIGN KEY (user_id, game) REFERENCES game.ratings` — тип игры в истории не дубль, а часть
    ссылки на строку рейтинга, join для фильтра по игре не нужен.
- `game.match_queue` — очередь поиска:
  - колонки `user_id`, `game`, `rating`, `joined_at`, `session_id` (заполняется, когда
    пара найдена);
  - первичный ключ `(user_id, game)` — встать в очередь дважды нельзя.
- В `game.sessions` добавляются `rated` и `resigned` (`boolean NOT NULL DEFAULT false`).

## Алгоритм
- **Elo.** Стартовый рейтинг 1200, ожидание `E = 1 / (1 + 10^((Rb − Ra) / 400))`.
  K = 40 в первых 20 партиях, дальше 24. Ничья даёт 0.5.
- **Начисление.** В `Store.save()`: если партия `rated` и только что стала завершённой (победа,
  ничья или время вышло), в той же транзакции блокируются строки рейтингов обоих игроков
  (`FOR UPDATE`, по порядку `user_id` — без дедлоков). Затем обновляются `game.ratings` и
  пишутся `game.rating_changes`. Повтор защищён первичным ключом.
- **Undo в рейтинговых партиях запрещён** — иначе рейтинг можно накручивать.
- **Подбор пары** — фоновый цикл в game-сервисе раз в 500 мс, как `RunClocks`:
  - Цикл работает под `pg_try_advisory_xact_lock`: при нескольких репликах подбирает пары
    одна.
  - Допуск по рейтингу растёт с временем ожидания: `±100` сразу, линейно до `±400` к моменту
    `T`. После `T` подходит любой соперник из очереди.
  - Порядок обработки: кто дольше ждёт, тот первым. Пара — кандидат с минимальной разницей
    рейтинга, попадающий в допуск обоих.
  - `T = clamp(5 с, 30 с, 10 с × √(active / 20))`, где `active` — игроки в очереди плюс игроки
    в незавершённых партиях за последние 10 минут. Это сигнал из БД самого game-сервиса: у
    presence нет глобального счётчика онлайна, а тянуть Redis ради одного числа — лишняя
    зависимость. При 20 активных `T = 10 с`, при 2 — около 5 с, при 200 — около 30 с.
  - Когда пара найдена, в одной транзакции создаётся сессия (`rated = true`, оба игрока, часы
    запущены). Обеим записям очереди проставляется `session_id`, отправляется
    `pg_notify('game_match_found', user_id)`.
- **Выход из партии** — `LeaveSession(session_id)`: партию в ожидании соперника создатель
  удаляет (`cancelled = true`), в идущей партии игрок сдаётся — победа сопернику, `resigned = true`,
  в рейтинговой начисляется рейтинг. В UI — «Cancel game» / «Resign» с подтверждением.
- **Брошенный поиск.** Клиент держит WS-подписку и шлёт heartbeat каждые 2 с. Запись без heartbeat
  дольше 15 с не участвует в подборе, дольше 60 с — удаляется.
- **Если в очереди никого нет**, игрок ждёт, пока кто-нибудь не встанет или он сам не
  отменит. Ботов в очереди нет.

## API (`core/shared/proto/axon/game/v1/game.proto`, additive)
- RPC:
  - `FindMatch(game, request_id) → MatchTicket` — встать в очередь, идемпотентно;
  - `CancelMatch(game)`;
  - `GetMatch(game) → MatchTicket` — состояние после перезагрузки страницы;
  - `GetRatings(user_ids, game) → map<user_id, Rating>`.
- В `Session` добавляются `optional bool rated` и `repeated RatingChange rating_changes`
  (заполняются после завершения партии).
- В `MatchTicket`: `joined_at`, `window`, `deadline`, `optional session_id`.
- Realtime: в существующий `GET /ws` добавляется подписка `match`. Сервер шлёт событие
  `match_found { session_id }` по `game_match_found`. Без WebSocket клиент может опрашивать
  `GetMatch`.
- Gateway: новые методы проксируются тем же способом, что текущие методы `GameService`.
  Rate limit на `FindMatch` — как на создание сессии.

## Затрагиваемые файлы
- `core/services/game/migrations/000003_ratings_matchmaking.{up,down}.sql`
- `core/services/game/internal/rating/elo.go` — чистая функция расчёта, table-driven тесты.
- `core/services/game/internal/matchmaking/{queue.go,matcher.go,window.go}` — очередь,
  цикл подбора, формулы допуска и `T`. Чистые функции тестируются отдельно, матчер — на
  настоящем Postgres.
- `core/services/game/internal/session/store.go` — `rated`, начисление в `save()`, запрет undo.
- `core/services/game/internal/server/{handler.go,socket.go}`, `cmd/game/main.go` — RPC, WS,
  запуск матчера.
- `core/shared/proto/axon/game/v1/game.proto`, затем `make proto`.
- `core/services/gateway/internal/proxy/*` — новые методы.
- `ui/web/src/components/games/tictactoe/{lobby.tsx,start-dialog.tsx,match.tsx,practice.tsx,arena.tsx}`,
  `ui/web/src/components/games/catalogue.tsx`, `ui/web/src/lib/query/games.ts`,
  `ui/web/src/lib/games/tictactoe-profiles.ts` (удалить выдуманные профили).
- `core/services/game/e2e/` и `ui/web/e2e/tic-tac-toe.spec.ts` — e2e.

## Как проверяется
- `go test ./...` в `core/services/game`: Elo (победа, поражение, ничья, K для новичков),
  допуск и `T` (граничные значения), матчер на Postgres:
  - близкая пара важнее старшего по ожиданию;
  - после `T` берётся любой;
  - нет двойного матча при конкурентных `FindMatch`;
  - отмена до матча;
  - рейтинг начисляется ровно один раз, в том числе при окончании по времени.
- `golangci-lint`, `buf lint`.
- Web: `npm run typecheck && npm run lint && npm test`.
- Docker e2e (`make test-web-e2e`): два пользователя нажимают Quick match → оба попадают в одну
  партию → играют до победы → в лобби у обоих видно `+N` / `−N`, рейтинги изменились
  симметрично. Отдельный сценарий — отмена поиска. Server e2e в `services/game/e2e` повторяет то
  же через Connect и WebSocket.
- Живая проверка на `https://axon.test` после пересборки game, gateway и web.

## Критерий готовности
Два игрока, нажавшие Quick match, попадают в одну рейтинговую партию, а после её окончания
рейтинг каждого меняется по Elo и виден в лобби.

## Расхождения с замыслом (по ходу работы)
- Файлы матчмейкинга: чистые функции в `internal/matchmaking/matchmaking.go`, очередь и цикл подбора —
  в `internal/session/queue.go` (им нужна транзакция `Store`), начисление — `internal/session/rating.go`.
- Канал уведомлений — `game_match_changed`, событие WS — `SessionEvent.match` с полным `MatchTicket`
  (статус `MATCHED` + `session_id`), а не отдельный `match_found`.
- Отдельного server e2e в `services/game/e2e` нет: новые RPC (`FindMatch`, `CancelMatch`, `GetMatch`,
  `GetRatings`, `LeaveSession`) проходят через gateway в web e2e `tic-tac-toe.spec.ts`
  (Quick match двух игроков с проверкой ±20, отмена поиска, отмена ожидающей партии, сдача).
- «Play again» в рейтинговой партии ведёт в лобби с `?play=quick` и сразу запускает поиск.
