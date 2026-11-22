import XCTest

final class PracticeFlowTests: XCTestCase {
    @MainActor
    func testPracticeOnOneDeviceUntilXWinsThenBeatTheComputerOpening() async throws {
        continueAfterFailure = false
        let app = launchApp(gateway: try E2EGateway.baseURL())
        try await register(.random(prefix: "practice"), in: app)

        try await openPractice("tictactoe", in: app)
        let opponent = try await existing(app.segmentedControls[AccessibilityID.Practice.opponent], "the opponent picker")
        opponent.buttons["Two players"].tap()
        let status = app.staticTexts[AccessibilityID.Practice.status]
        try await label(of: status, is: "X to move")

        for (index, cell) in [0, 3, 1, 4, 2].enumerated() {
            let button = try await existing(app.buttons[AccessibilityID.Game.ticTacToeCell(cell)], "cell \(cell)")
            button.tap()
            try await label(of: button, is: index.isMultiple(of: 2) ? "X" : "O")
        }
        try await label(of: status, is: "X wins")
        try await existing(app.descendants(matching: .any)[AccessibilityID.Practice.result], "the result card")
        attachScreenshot("Two players", of: app)

        app.buttons[AccessibilityID.Practice.again].tap()
        try await label(of: status, is: "X to move")
        opponent.buttons["Hard"].tap()
        try await label(of: status, is: "Your move")
        app.buttons[AccessibilityID.Game.ticTacToeCell(4)].tap()
        try await label(of: status, is: "Your move", timeout: 10)
        let answered = (0..<9).filter { app.buttons[AccessibilityID.Game.ticTacToeCell($0)].label == "O" }
        XCTAssertEqual(answered.count, 1)
        attachScreenshot("Against the computer", of: app)

        try await existing(app.buttons[AccessibilityID.Practice.undo], "undo").tap()
        try await until(
            app.buttons[AccessibilityID.Game.ticTacToeCell(4)], "the centre to be empty again", NSPredicate(format: "label != 'X'")
        )
    }

    @MainActor
    private func openPractice(_ game: String, in app: XCUIApplication) async throws {
        await dismissSavePasswordPrompt(in: app, within: 5)
        try await existing(app.buttons[AccessibilityID.Lobby.game(game)], "the \(game) catalogue row").tap()
        try await existing(app.buttons[AccessibilityID.Lobby.start], "the start button").tap()
        try await existing(app.buttons[AccessibilityID.Lobby.practice(game)], "the practice option").tap()
        try await existing(app.descendants(matching: .any)[AccessibilityID.Practice.root], "the practice screen")
    }
}
