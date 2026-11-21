# Шашки в SwiftUI-приложении

## Контекст
Сервер и веб умеют шашки (PR #34), а приложение после PR #35 прячет игры, для которых у него нет
доски. Нативный клиент — часть цели проекта, поэтому шашки должны появиться в приложении так же,
как крестики, шахматы и кворидор: партия видна в лобби, играется, смотрится зрителем и в повторе.

Результат: в лобби есть карточка «Checkers», партия против второго игрока идёт через сокет до победы,
серия взятий собирается касаниями и уходит одним ходом, повтор листается по кадрам.

## Решение
- **Модель.** `GameKind.checkers` (места «Black»/«White», ничья по соглашению как в шахматах,
  отмены хода нет), typealias-ы `CheckersBoardState`/`CheckersPieceCode`/`CheckersEnd`.
- **Домен доски** `Features/Games/Checkers/` — только разбор и отображение, правил нет:
  - `CheckersNotation` — поля 1–32 и их геометрия (как `Square.Row/Col` в Go и `rowOf/colOf` в
    вебе), разворот на 180° для второго игрока, разбор и печать `11-15` / `22x15x6` с теми же
    ограничениями, что у сервера (3–38 символов, 2–13 полей, без ведущих нулей).
  - `CheckersPosition` — 32 `CheckersPiece` из proto, при пустой доске — стартовая расстановка.
  - `CheckersBoardModel` — чистое состояние доски: фигуры с ходом, выбранный путь, цели, след
    последнего хода, подписи VoiceOver и разбор касания. Касание берёт ходы только из
    `legal_moves` сервера: выбор фигуры, продолжение серии по промежуточным полям, касание
    конечного поля сразу отправляет ход, если он единственный.
- **Отрисовка.** `CheckersBoardView` (кнопка на каждое тёмное поле, номер поля в углу, кольцо у
  фигур с ходом, точки целей) и `CheckersPieceView` (диск, кольцо, корона дамки). Каждое поле —
  отдельный элемент доступности: «14, black king, can move», действия VoiceOver «Capture 31 to 22
  to 29» на фигуре с ходом. Перспектива: место 0 (чёрные) и зрители видят чёрных снизу, белые —
  доску, развёрнутую к себе.
- **Партия и повтор.** `GameBoard`, `GameViewModel.checkersMove` (`SessionCommand.checkers_move`
  с `ply`/`revision`), список ходов парами, `SessionStatusText`: «by blockade» / «by capturing
  every piece», «forty-move rule», «move limit», подсказка «You must capture» в свой ход при
  обязательном взятии. `ReplayViewModel` берёт ходы из `checkers.moves`, кадры — `HistoryFrame.checkers`.
- **XCUITest.** Общие шаги (регистрация, создание партии, ожидания) вынесены в `AppFlow.swift`,
  `Opponent` научился `checkers_move`. `CheckersFlowTests` играет партию из 21 полухода из Go e2e:
  приложение за чёрных касаниями (включая `16x23x30` с превращением и тройное `14x21x30x23`),
  соперник — из теста через сокет; победа блокировкой и повтор.

## Файлы
- `ui/app/Core/Models/Models.swift`, `ui/app/Shared/AccessibilityID.swift`.
- `ui/app/Features/Games/Checkers/{CheckersNotation,CheckersPosition,CheckersBoardModel,CheckersBoardView,CheckersPieceView}.swift`.
- `ui/app/Features/Game/{GameBoard,GameView,GameViewModel,SessionStatusText}.swift`,
  `ui/app/Features/Games/Common/MoveListView.swift`, `ui/app/Features/Replay/ReplayViewModel.swift`.
- Тесты: `ui/app/Tests/AxonTests/Boards/{CheckersNotationTests,CheckersBoardModelTests,GameScreenSnapshotTests}.swift`,
  дополнения в `BoardInteractionTests`, `BoardSnapshotTests`, `GameComponentTests`, `SessionCodecTests`,
  `GameViewModelTests`, `LobbyAndAuthTests`; `ui/app/UITests/AxonUITests/{AppFlow,CheckersFlowTests}.swift`,
  `Opponent.swift`, `TicTacToeFlowTests.swift`.

## Как проверяется
- `make proto && make app-lint` — 0 нарушений в `--strict`.
- `make app-test` — unit-тесты на macOS зелёные, снимки досок и экрана партии в `.local/app/snapshots/`.
- `make app-build` — сборка под iOS Simulator.
- `make test-e2e-app` — `TicTacToeFlowTests`, `OpponentSmokeTests`, `CheckersFlowTests` зелёные на
  iPhone-симуляторе против стенда `axon-app-e2e`, скриншоты доски в xcresult; `make app-e2e-down`.

## Критерий готовности
`CheckersFlowTests` проходит партию в шашки в приложении против живого соперника до победы и повтора.

## Результаты (2026-11-21)
- `make app-lint` — 0 нарушений в `--strict`.
- `make app-test` (macOS) — 189 тестов в 33 сьютах зелёные, из них по шашкам: нотация (разбор,
  печать, отказ на 20 неверных строках, самая длинная серия), геометрия полей и разворот, позиция из
  proto и из JSON-кадра сервера, модель доски (выбор, серия через промежуточные поля и сразу по
  конечному, подписи VoiceOver), касания мышью в `NSWindow` (`31x22x13`, `14x21x30x23`, `10-15` на
  развёрнутой доске, без хода в чужой ход), снимки доски и экрана партии на macOS в обеих темах,
  команда `checkers_move`, итоги и подсказка, повтор. С `TEST_RUNNER_AXON_GATEWAY_URL` живой
  `LiveGatewayTests` проходит против стенда `axon-app-e2e`.
- `make app-build` (iOS Simulator) — сборка без ошибок.
- `make test-e2e-app` — 3/3 XCUITest зелёные на iPhone 18 Pro: `CheckersFlowTests` (21 полуход,
  превращение, двойные и тройное взятие касаниями, «You won» · «by blockade», повтор «Move 21 of 21»
  → «Start», 1 мин 59 с), `TicTacToeFlowTests` (после выноса шагов в `AppFlow`) и
  `OpponentSmokeTests`. Стенд снесён `make app-e2e-down`.
- Скриншоты: лобби с карточкой Checkers, тройное взятие в процессе, победа и повтор — в xcresult;
  экран партии на macOS (широкая раскладка) — `.local/app/snapshots/checkers-game-macos-*.png`.

### Расхождения с замыслом
- Окно macOS не снималось вживую против стенда: экран партии на macOS рендерится тестом в
  настоящем `NSWindow` на fake-сети (`GameScreenSnapshotTests`), сеть macOS-клиента проверена
  `LiveGatewayTests`.
