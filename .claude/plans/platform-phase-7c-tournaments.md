# Фаза 7c — турниры как сага

## Контекст
Партии в Axon сейчас разовые: матчмейкинг или приглашение по ссылке. Турнир связывает десятки
партий в одну длинную бизнес-транзакцию, которая живёт минуты и часы, переживает падения
сервисов и не может держать состояние в памяти. Это каноничный пример саги поверх уже
построенной цепочки outbox → Debezium → Kafka: команды в game идут событиями, результаты
приходят из `game.finished.v1`, каждый шаг идемпотентен, таймауты — строки в таблице.

Результат: восемь игроков регистрируются, создатель запускает олимпийку, game сам создаёт
партии каждого раунда, неявившийся игрок получает техническое поражение по дедлайну, турнир
доходит до победителя и итоговой таблицы; повторные доставки событий и рестарт оркестратора
посреди раунда ничего не дублируют и не теряют. В вебе — список турниров, создание, страница
турнира с регистрациями, сеткой/таблицей и ссылками на партии.

## Решение

### Сервис и хранение
- Модуль `core/services/tournament` в `go.work`, схема и роль `tournament`/`tournament_service`
  (`01-schemas.sh`, `TOURNAMENT_DB_PASSWORD`, `TOURNAMENT_POSTGRES_DSN`).
- Таблицы: `tournaments` (формат, ёмкость, раунды, статус, текущий раунд, победитель,
  создатель, времена), `registrations` (рейтинг на момент регистрации, посев, статус
  `registered`/`returned`), `rounds` (начало/конец), `matches` (раунд, доска, сессия, места,
  результат `pending`/`win`/`draw`/`bye`/`aborted`, победитель, `forfeit`), `deadlines`
  (дедлайн неявки по сессии, `fired_at`), `ratings` (проекция `rating.changed.v1`),
  `processed_events`, `outbox`.
- Рейтинг для посева и швейцарки берётся из собственной проекции `rating.changed.v1`
  (по умолчанию 1200, как у новичка в rating и game), без синхронного похода в game при регистрации.

### Форматы — чистые функции (`internal/pairing`)
- **Олимпийка** на 4/8/16 мест: сетка — ближайшая степень двойки от числа игроков, стандартный
  порядок посева (1–8, 4–5, 2–7, 3–6), пустые места — «bye» лучшим посевам. Место 0 (первый ход)
  — у лучшего посева. Ничья в олимпийке — проходит лучший посев (переигровки не делаем: иначе
  крестики-нолики могут не закончиться никогда).
- **Швейцарка** на N раундов: сортировка по очкам, затем рейтингу, затем посеву; пары сверху
  вниз с перебором с возвратом, чтобы не было повторных встреч; при нечётном числе — bye
  последнему, у кого его ещё не было. Очки в половинках (победа и bye 2, ничья 1) — без float.
  Раундов не больше, чем игроков минус один.

### Сага (`internal/saga` — чистый автомат, `internal/orchestrator` — шаги)
- Состояния: `registration → seeding → running(раунд k идёт) → running(раунд k закончен) → …
  → finished | cancelled`. Каждый шаг — одна транзакция: строка турнира берётся `FOR UPDATE`,
  чистая функция считает новое состояние и эффекты, состояние и события outbox пишутся в той же
  транзакции. Id событий детерминированы (uuid v5 от турнира/раунда/сессии), outbox пишет
  `ON CONFLICT DO NOTHING` — повтор шага ничего не публикует второй раз.
- **Старт**: `StartTournament` переводит в `seeding` и коммитит; следующий шаг считает посев и
  пары первого раунда, пишет `tournament.round_started.v1` и дедлайны неявки.
- **Создание партий**: game слушает `tournament.round_started.v1` (группа `game-tournaments`) и
  создаёт рейтинговые сессии с id `uuid v5(турнир, раунд, доска)` (`core/shared/pkg/tournamentid`,
  game пересчитывает id и отбрасывает событие при расхождении). Повторная доставка попадает на
  уже существующую сессию владельца и ничего не создаёт. Сессия помечена `tournament_id`
  (колонка в `game.sessions`, поле снимка журнала, `optional string tournament_id` в
  `Session`).
- **Результаты**: только из `game.finished.v1`, который теперь несёт `optional string
  tournament_id = 10` и `optional uint32 plies = 11`. Турнир пишет результат по `session_id`
  вместе с `processed_events`; завершение последней партии фиксирует конец раунда, следующий
  шаг (в отдельной транзакции) делает пары следующего раунда или итог `tournament.finished.v1`.
- **Неявка**: дедлайн = начало раунда + `TOURNAMENT_NO_SHOW_TIMEOUT` (по умолчанию 2 мин).
  Подметальщик раз в `TOURNAMENT_SWEEP_INTERVAL` берёт просроченные строки `FOR UPDATE SKIP
  LOCKED`; если партия ещё не решена — пишет команду `tournament.forfeit.v1`. Game завершает
  сессию сдачей отсутствующего — игрока, чей ход и кто ещё не сделал ни одного хода
  (`ply ≤ seat`); если оба уже ходили, команда ничего не делает. Результат всё равно приходит
  только из `game.finished.v1`; техническим поражение считается, когда победа случилась при
  `plies < 2`. Команда несёт пару игроков, поэтому game сначала идемпотентно создаёт сессию —
  порядок между топиками не важен.
- **Отмена**: до старта — создатель или админ, регистрации возвращаются (`returned`),
  `tournament.cancelled.v1` со списком игроков. Во время игры — только админ: незавершённые
  матчи становятся `aborted`, событие несёт их сессии, game записывает турнир в
  `game.cancelled_tournaments` и отменяет сессии (`FactCancelled`, без `game.finished`);
  запоздавшие `round_started`/`forfeit` отменённого турнира game пропускает.
- **Устойчивость**: всё состояние в Postgres, offsets коммитятся только после транзакции.
  Подметальщик также подбирает турниры, застрявшие в `seeding` или с законченным, но не
  продвинутым раундом, — рестарт между шагами ничего не теряет.

### API и доступ
- `axon.tournament.v1.TournamentService`: `CreateTournament` (идемпотентно по `request_id`),
  `RegisterForTournament`, `WithdrawFromTournament`, `StartTournament`, `GetTournament`,
  `ListTournaments`, `CancelTournament`. JWT проверяется локально через JWKS.
- Старт и отмена — создатель или роль `admin`; роль проверяется вызовом `AuthService/GetMe` с
  токеном вызывающего только тогда, когда он не создатель (RBAC — данные auth, в JWT ролей нет).
- Gateway: прокси, записи в `policy` (create и start — `TierSensitive`, остальные — стандарт),
  `Get`/`List` в повторяемых процедурах, `TOURNAMENT_SERVICE_URL`.

### Метрики
`tournament_active` (seeding + running, из Postgres), `tournament_rounds_total`,
`tournament_forfeits_total`, `tournament_saga_step_seconds{step}` (seed/record/advance/forfeit/
cancel — от события-триггера до коммита шага), `tournament_events_total{result}`
(applied/duplicate/skipped/invalid).

### Web
Пункт «Tournaments» в разделе игр (`/games/tournaments`): список (TanStack Query, опрос),
диалог создания, страница турнира (`/games/tournaments/[id]`) с регистрацией/отменой/стартом,
сеткой олимпийки или таблицей швейцарки и раундами со ссылками на партии (смотреть можно
с 7a). Опрос каждые 2 с, пока турнир не закончен. Без описаний под заголовками, прокрутка —
`ScrollArea`.

## Файлы
- `core/shared/proto/axon/tournament/v1/tournament.proto`; `axon/events/v1/events.proto`
  (`TournamentRoundStarted`, `TournamentForfeit`, `TournamentCancelled`, `TournamentFinished`,
  поля `GameFinished`); `axon/game/v1/{game,journal,owner}.proto`.
- `core/shared/pkg/tournamentid/` — id сессии турнира.
- `core/services/game/`: `migrations/000010_tournaments.*`, `internal/session/{store,domain}.go`,
  `internal/journal/codec.go`, `internal/projection/{postgres,finished}.go`,
  `internal/owner/owner.go`, `internal/server/{handler,match,tournament}.go`,
  `internal/tournament/` (консьюмер команд, отменённые турниры), `cmd/game/main.go`,
  `integration/tournament_test.go`.
- `core/services/tournament/**`: `cmd/tournament`, `internal/{pairing,saga,store,orchestrator,
  consumer,server}`, `migrations/000001_tournaments.*`, `integration/`, `e2e/`.
- `core/services/gateway/{cmd/gateway/main.go,internal/proxy/tournament.go,internal/policy/*,
  internal/config/config.go}`.
- `core/deploy/`: `docker-compose.yml` (`tournament`, `tournament-migrate`, пароль роли,
  зависимость `cdc-setup`), `docker-compose.{ha,mtls,mtls-e2e,observability,backup,e2e,web-e2e}.yml`,
  новый `docker-compose.tournament-e2e.yml`, `cdc/setup.sh` и `cdc/outbox-connector.json`
  (`tournament.outbox`), `postgres/init/01-schemas.sh`, `.env.example`,
  `observability/prometheus/prometheus.yml`.
- Пароль роли во всех integration-тестах с `01-schemas.sh` (`auth`, `chat`, `game`, `analysis`,
  `tournament`).
- `Makefile`, `core/deploy/docker-compose.tools.yml`, `.github/workflows/ci.yml`,
  `scripts/start-dev.mjs`, `core/shared/e2e/security/mtls_test.go`.
- `ui/web/src/lib/connect/clients.ts`, `ui/web/src/lib/query/tournaments.ts`,
  `ui/web/src/components/games/tournaments/*`, `ui/web/src/app/(app)/games/tournaments/**`,
  `ui/web/e2e/tournament.spec.ts`.

## Как проверяется
- `make lint` — 0 замечаний; `go test -race` в `shared`, `game`, `gateway`, `tournament`.
- Unit: пары олимпийки и швейцарки (table-driven: посев, bye, без повторов, нечётное число),
  переходы автомата и ошибки, компенсации (отмена до/после старта, неявка, ничья в олимпийке),
  консьюмер game на фейковых сессиях (повтор, отменённый турнир, неверный id), `Forfeit`/`Abort`
  в правилах game, кодек журнала с `tournament_id`, политика gateway, сервер (права создателя
  и админа).
- Integration (`go test -tags integration`, testcontainers Postgres + Kafka):
  `services/tournament/integration` — сага на олимпийке через Kafka с дублями `game.finished.v1`,
  рестарт оркестратора посреди раунда, шаг между фиксацией раунда и продвижением, неявка по
  дедлайну, отмены; `services/game/integration/tournament_test.go` — дубль `round_started`
  не создаёт вторых сессий, `forfeit` до `round_started`, отмена.
- E2E: `make test-e2e-tournament` — проект `axon-tournament-e2e` на базовом стеке + CDC,
  `TOURNAMENT_NO_SHOW_TIMEOUT=30s`: 8 игроков регистрируются через gateway, старт, все партии
  крестиков-ноликов играются через сокет, восьмой посев не приходит и получает техническое
  поражение, турнир `finished` с победителем — первым посевом и ожидаемой таблицей; затем
  `down -v`.
- Web: `tsc`, `eslint`; Playwright `e2e/tournament.spec.ts` на `docker-compose.web-e2e.yml`
  (проект `axon-web-e2e-p7c`): создание → регистрация → старт → после сыгранной партии сетка
  показывает победителя; затем `down -v`.

## Критерий готовности
Турнир на восемь игроков, запущенный через gateway, доходит до `finished` с победителем и
таблицей, неявившийся получает техническое поражение по дедлайну из таблицы, а дубли событий и
рестарт оркестратора посреди раунда не создают лишних партий и строк.

## Результаты (2026-11-20)
- `golangci-lint run` по всем модулям (`--timeout=25m`, теги `e2e`/`integration`) — 0 замечаний
  после исправления двух (неиспользуемый хелпер в `store`, самосравнение в тесте).
  `go test -race` зелёный в `shared`, `game`, `gateway`, `tournament`.
- Integration `services/tournament/integration` (testcontainers Postgres 17 + Kafka 7.5):
  олимпийка на 8 игроков с рестартом оркестратора после двух досок первого раунда дошла до
  `finished` за 6.6 с: 18 доставок `game.finished.v1` (дубли с тем же и с новым `event_id`,
  результат по уже решённой доске, доставки пока оркестратор лежит), применено 7, дублей 11,
  в outbox ровно 3 `round_started` и 1 `finished`, `processed_events` без лишних строк. В
  одном прогоне Kafka после рестарта переотдал одну незакоммиченную запись, её посчитали
  дублем (19 обработок на 18 отправок). Застрявшие `seeding` и законченный, но не продвинутый
  раунд подхватываются подметальщиком. Дедлайн неявки 2 с при двух подметальщиках дал ровно
  одну команду `forfeit`, не позже 9 мс после дедлайна. Отмена до и во время игры работает.
- Integration `services/game/integration/tournament_test.go`: четыре доставки `round_started`
  и `forfeit`, пришедший раньше `round_started`, дают 3 сессии и одно техническое поражение
  (`game.finished` с `tournament_id`, `plies=0`). Двойная отмена оставляет одну сессию и
  один `game.finished`.
- E2E `make test-e2e-tournament` (проект `axon-tournament-e2e`, `TOURNAMENT_NO_SHOW_TIMEOUT=30s`,
  после прогона `down -v`): 8 игроков через gateway, повторный `CreateTournament` с тем же
  `request_id` вернул тот же турнир, девятый игрок получил `FailedPrecondition`, старт не
  создателем — `PermissionDenied`. От старта до `finished` 39.4 с. Первый раунд закрылся на
  +36.3 с, потому что ждал дедлайн неявки 30 с. Второй и третий раунды открылись на +36.2 с и
  +37.7 с. Восьмой посев не пришёл. Посев 1 сделал первый ход и выиграл техническим поражением
  соперника: сессия `WON`, `resigned`, матч `forfeit=true`, через 5.5 с после дедлайна (команда
  и `game.finished` проходят через CDC, первая команда ещё и создаёт топик). Победитель —
  посев 1. Таблица: места 1, 2, 3, 3, 5, 5, 5, 5 по порядку посева. Метрики:
  `tournament_rounds_total 3`, `tournament_forfeits_total 1`, `tournament_active 0`.
  `tournament_saga_step_seconds` (время от события до коммита шага): seed 1 шаг за 3.5 мс,
  record 7 шагов со средним 2.8 с (почти всё — CDC на загруженной VM), advance 3 шага со
  средним 0.8 с, forfeit 1 шаг за 0.25 с.
- Web: `tsc` чистый, `eslint` — 0 ошибок (3 старых предупреждения в calls/chat).
  Playwright `e2e/tournament.spec.ts` на `axon-web-e2e-p7c`: создание через диалог → регистрация
  двух игроков → старт → партия в финале через UI → у третьего зрителя без перезагрузки карточка
  финала показывает победителя, баннер и таблица обновляются. Тест прошёл за 41 с, после
  исправлений повторён (1.2 мин вместе со сборкой), затем `down -v`.
- Отличие от замысла: ссылка «Open game» на карточке матча появляется только тогда, когда game
  уже создал сессию (`useTournamentGame` опрашивает `GetSession`). Иначе первый клик сразу после
  старта открывает экран приглашения: `SessionLoader` не повторяет запрос после NotFound.
- Не проверено: швейцарка end-to-end (только unit и пары), отмена турнира админом во время игры
  через gateway (только integration и unit сервера), overlay `ha`/`mtls`/`observability` с
  турниром (только `docker compose config`), локальный `npm run start:dev` с турниром.
