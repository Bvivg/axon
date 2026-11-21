import XCTest

final class TicTacToeFlowTests: XCTestCase {
    @MainActor
    func testRegisterPlayAgainstALiveOpponentAndReplay() async throws {
        continueAfterFailure = false
        let gateway = try E2EGateway.baseURL()
        let app = launchApp(gateway: gateway)

        try await register(.random(prefix: "app"), in: app)

        let sessionID = try await createGame("tictactoe", in: app)
        let opponent = Opponent(baseURL: gateway)
        try await opponent.register()
        _ = try await opponent.joinSession(sessionID)
        let socket = try await opponent.connect(to: sessionID)
        addTeardownBlock { await socket.close() }

        try await playTopRowToWin(in: app, against: socket)
        try await replayFromTheEndToTheStart(in: app)
    }

    @MainActor
    private func playTopRowToWin(in app: XCUIApplication, against opponent: GameSocket) async throws {
        let status = app.staticTexts[AccessibilityID.Game.status]
        try await label(of: status, is: "Your turn")

        try await tap(cell: 0, in: app)
        try await opponent.play(cell: 3, atPly: 1)
        try await label(of: cell(3, in: app), is: "O")
        attachScreenshot("Game", of: app)

        try await label(of: status, is: "Your turn")
        try await tap(cell: 1, in: app)
        try await opponent.play(cell: 4, atPly: 3)
        try await label(of: cell(4, in: app), is: "O")

        try await label(of: status, is: "Your turn")
        try await tap(cell: 2, in: app)
        try await label(of: status, is: "You won")
    }

    @MainActor
    private func replayFromTheEndToTheStart(in app: XCUIApplication) async throws {
        try await existing(app.buttons[AccessibilityID.Game.replay], "the replay button").tap()
        let position = app.staticTexts[AccessibilityID.Replay.position]
        try await label(of: position, is: "Move 5 of 5")
        attachScreenshot("Replay", of: app)
        try await existing(app.buttons[AccessibilityID.Replay.first], "the first-move button").tap()
        try await label(of: position, is: "Start")
    }

    @MainActor
    private func cell(_ index: Int, in app: XCUIApplication) -> XCUIElement {
        app.buttons[AccessibilityID.Game.ticTacToeCell(index)]
    }

    @MainActor
    private func tap(cell index: Int, in app: XCUIApplication) async throws {
        let button = try await existing(cell(index, in: app), "cell \(index)")
        try await until(button, "cell \(index) to be enabled", NSPredicate(format: "isEnabled == true"))
        button.tap()
        try await label(of: button, is: "X")
    }
}
