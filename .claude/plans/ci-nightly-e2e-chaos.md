# Nightly e2e, `make chaos` и `make bench`

## Контекст
Главные гарантии платформы (CDC exactly-once, mTLS и ротация ключей, сага турнира, хаос без
потерянных ходов, цифры нагрузки) доказаны разовыми ручными прогонами. В CI на каждый PR идёт
только `test-e2e` (auth + chat), остальные e2e — ручные make-цели. Хаос из фазы 3 — скрипты без
проверок, сверка с read model — python-скрипт в git-ignored `.local/evidence`. На вопрос «держится
ли это на `main` сегодня?» ответа нет.

Результат: `make chaos` и `make bench` сами поднимают одноразовый стек, гоняют нагрузчик и
падают при потере подтверждённого хода; nightly-workflow каждую ночь гоняет флагманские e2e и
маленький хаос и выкладывает отчёты артефактами.

## Решение
- **Сверка — Go-команда `core/services/game/cmd/reconcile`**, а не python. Она читает
  `results.json` нагрузчика, ждёт, пока проектор дойдёт до конца журнала (`next_offset` ≥ end по
  каждой партиции), читает журнал Kafka через `journal.Kafka` с тем же `Fence`, что у проектора
  (ходы зомби-владельца не считаются, undo убирает ход, cancel — партию), и read model
  (`game.sessions` + `game.moves`). Падает, если у партии в журнале или read model меньше ходов,
  чем подтвердил клиент; если журнал и read model расходятся хоть по одной партии; если
  доигранная партия не совпала ход в ход или не завершена в read model; если доиграно меньше
  `-min-completed` (0,95) партий — иначе сломанный стенд дал бы зелёную сверку на нуле ходов.
- **Нагрузчик** генерирует игроков из seed (`-players`) и несёт партии чемпионов в бинаре
  (`legends.json`, `go:embed`), поэтому ему не нужны файлы из `.local`. Копия legends и генератор
  `users.json` в toolbox kind-хаоса (фаза 6) убраны — он берёт то же из нагрузчика.
- **Overlay `docker-compose.loadgen.yml`**: game с `GAME_TEST_HOOKS` и `APP_ENV=test`, раннеры
  `loadgen` и `reconcile` (golang-образ, `go run`, профиль `load`), host-порты сняты `!reset`, чтобы
  стенд не конфликтовал с dev-стеком и e2e.
- **`core/deploy/loadgen.sh bench|chaos <compose…>`**: поднимает game, гоняет нагрузчик, печатает
  ходы/с, p50/p95/p99, ошибки и итог сверки, пишет `loadgen.json`/`reconcile.json` в
  `REPORT_DIR` (по умолчанию `.local/<mode>`). При ошибке сохраняет логи стека в `e2e-logs.txt`,
  на выходе `down -v`, если не `KEEP=1`.
  - `bench` (`axon-bench`, 1 game): параметры фазы 1b — 6000 матчей, 1000 параллельно, пауза 260 мс, seed 11.
  - Перед нагрузкой раннеры компилируются отдельным `go build`, иначе первые сбои приходятся на
    компиляцию `go run`, а не на игру.
  - `chaos` (`axon-chaos`, base + chaos + loadgen, game ×2): 200 матчей, 10 параллельно,
    40 переподключений на игрока (передача партиций после `kill` занимает до ~15 с); задержка
    300 мс до Postgres на 15 с → graceful-рестарт game-1 → `docker kill` game-2 (новый шаг
    `scenario.sh kill`). Если нагрузка кончилась раньше последнего сбоя — прогон падает.
- **`.github/workflows/nightly.yml`** (`schedule` + `workflow_dispatch`): матрица
  `test-e2e-security`, `test-rating-e2e`, `test-e2e-game`, `test-e2e-analysis`, `test-e2e-tournament`
  и отдельная задача хаоса (unit-тест сверки, затем `make chaos`). Логи стека — артефакт при
  падении, отчёт хаоса — всегда. Чтобы логи было что выкладывать, флагманские цели при падении
  пишут логи стека в `e2e-logs.txt` до `down -v` (как `test-e2e`).
- **Остатки spike/probe не удалены**: все пять путей из ревью живые. `playwright.calls-spike.config.ts`
  — базовый конфиг всех calls-конфигов Playwright; `Dockerfile.calls-spike` и
  `docker-compose.calls-spike.yml` собирают media-образ Playwright и `init` для CI-задачи
  `calls-e2e`; `/calls/probe` открывает `calls-ws-support.ts` (CI), `/calls/spike` —
  calls-admission e2e. Нужен отдельный переезд с переименованием и прогоном calls e2e.

## Файлы
- `core/services/game/cmd/reconcile/{main,reconcile,reconcile_test}.go`
- `core/services/game/cmd/loadgen/{main,scripts}.go`, `legends.json`
- `core/deploy/docker-compose.loadgen.yml`, `core/deploy/loadgen.sh`, `core/deploy/chaos/scenario.sh`,
  `core/deploy/docker-compose.chaos.yml` (все Postgres-джобы ждут toxiproxy: под overlay `postgres` —
  его алиас, и `auth-migrate` падал с `no such host`)
- `core/deploy/k8s/toolbox/chaos-load.sh`, удалён `core/deploy/k8s/toolbox/legends.json`
- `Makefile` (`bench`, `chaos`, `e2e-teardown`), `.github/workflows/{nightly,ci}.yml`

## Как проверяется
- `go test -race ./cmd/...` в game: таблица `compare` и журнал на `journal.Memory`; мутации
  (без fence, без undo, без cancel, без проверки потерь) роняют тесты.
- `golangci-lint`, `actionlint`, `shellcheck`, `docker compose … config -q` для обоих стеков.
- `make chaos` локально: зелёный, стека `axon-chaos` и его томов после прогона нет.
- `make bench MATCHES=300 CONCURRENCY=100 KEEP=1`, затем удалить одну строку `game.moves` и
  перезапустить `reconcile` — выход 1, `sessions_with_lost_moves` = 1; затем `down -v`.
- Одна цель матрицы локально так же, как в workflow (`buf generate`, `make test-e2e-game`).

## Критерий готовности
`make chaos` падает на потерянном подтверждённом ходе и зелёный на `main`, а nightly гоняет
флагманские e2e и хаос без ручных шагов.

## Результаты (2026-11-21, локально, Docker Desktop 10 ядер)
| Прогон | Итог |
|---|---|
| `make chaos` (200 матчей, 10 параллельно) | 200/200 доиграно; подтверждено 6552 хода = журнал 6552 = read model 6552; потерь 0, расхождений 0; 151 с; `axon-chaos`, его тома и контейнеры удалены |
| `make chaos`, 20 переподключений на игрока | 195/200, 5 × `socket closed: 4001` после `kill`; потерь 0 (6425 = 6425 = 6425) — отсюда 40 переподключений |
| `make bench MATCHES=300 CONCURRENCY=100` | 0 сбоев, 10 216 ходов за 42 с, p50/p95/p99 2/20/70 мс; 10 216 = 10 216 = 10 216 |
| `reconcile` после удаления одной строки `game.moves` | выход 1: `sessions_with_lost_moves` 1, `sessions_diverged` 1 |
| `make test-e2e-game` как в nightly (`buf generate`, затем цель) | PASS, 42 с, стек удалён |

Не проверено: сам workflow на GitHub Actions (первый прогон — после мержа, `workflow_dispatch`),
остальные четыре цели матрицы локально, kind-хаос после замены legends (нужен кластер).
