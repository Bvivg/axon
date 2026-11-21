import XCTest

final class CheckersFlowTests: XCTestCase {
    private static let blockadeWin = """
    9-14 23-18 14x23 26x19 11-16 30-26 16x23x30 21-17 30x21x14 31-26 10-15 \
    24-19 15x24x31 29-25 12-16 22-17 14x21x30x23 32-27 31x24 28x19x12 5-9
    """.split(separator: " ").map(String.init)

    private static let tripleJumpPly = 16

    @MainActor
    func testPlayMultiJumpsToABlockadeWinAndReplay() async throws {
        continueAfterFailure = false
        let gateway = try E2EGateway.baseURL()
        let app = launchApp(gateway: gateway)

        try await register(.random(prefix: "chk"), in: app)

        let sessionID = try await createGame("checkers", in: app)
        let opponent = Opponent(baseURL: gateway)
        try await opponent.register()
        _ = try await opponent.joinSession(sessionID)
        let socket = try await opponent.connect(to: sessionID)
        addTeardownBlock { await socket.close() }

        let status = app.staticTexts[AccessibilityID.Game.status]
        try await label(of: status, is: "Your turn")
        attachScreenshot("Checkers opening", of: app)

        for (ply, move) in Self.blockadeWin.enumerated() {
            if ply.isMultiple(of: 2) {
                if ply == Self.tripleJumpPly {
                    try await existing(app.staticTexts["You must capture"], "the capture hint")
                }
                try await play(move, in: app, midway: ply == Self.tripleJumpPly ? "Checkers triple jump" : nil)
            } else {
                try await socket.play(checkers: move, atPly: UInt32(ply))
                try await occupied(Self.squares(of: move).last ?? 0, by: "white", in: app)
            }
        }

        try await label(of: status, is: "You won")
        try await existing(app.staticTexts["by blockade"], "the blockade detail")
        attachScreenshot("Checkers won", of: app)
        try await replayFromTheEndToTheStart(in: app)
    }

    @MainActor
    private func replayFromTheEndToTheStart(in app: XCUIApplication) async throws {
        try await existing(app.buttons[AccessibilityID.Game.replay], "the replay button").tap()
        let position = app.staticTexts[AccessibilityID.Replay.position]
        try await label(of: position, is: "Move \(Self.blockadeWin.count) of \(Self.blockadeWin.count)")
        try await label(of: app.staticTexts[AccessibilityID.Replay.result], is: "Black won by blockade")
        try await occupied(9, by: "black", in: app)
        attachScreenshot("Checkers replay", of: app)
        try await existing(app.buttons[AccessibilityID.Replay.first], "the first-move button").tap()
        try await label(of: position, is: "Start")
        try await occupied(22, by: "white", in: app)
    }

    @MainActor
    private func play(_ move: String, in app: XCUIApplication, midway screenshot: String?) async throws {
        let path = Self.squares(of: move)
        guard let origin = path.first, let destination = path.last else { return }
        let first = square(origin, in: app)
        try await until(first, "\(origin) to be movable", NSPredicate(format: "label CONTAINS %@", "can move"))
        first.tap()
        try await until(first, "\(origin) to be selected", NSPredicate(format: "label CONTAINS %@", "selected"))
        for step in path.dropFirst().dropLast() {
            let button = square(step, in: app)
            button.tap()
            try await until(button, "\(step) on the path of \(move)", NSPredicate(format: "label CONTAINS %@", "selected"))
        }
        if let screenshot {
            attachScreenshot(screenshot, of: app)
        }
        square(destination, in: app).tap()
        try await occupied(destination, by: "black", in: app)
    }

    @MainActor
    private func occupied(_ number: Int, by side: String, in app: XCUIApplication) async throws {
        try await until(
            square(number, in: app),
            "a \(side) piece on \(number)",
            NSPredicate(format: "label BEGINSWITH %@", "\(number), \(side)")
        )
    }

    @MainActor
    private func square(_ number: Int, in app: XCUIApplication) -> XCUIElement {
        app.buttons[AccessibilityID.Game.checkersSquare(number)]
    }

    private static func squares(of move: String) -> [Int] {
        move.split(whereSeparator: { $0 == "-" || $0 == "x" }).compactMap { Int($0) }
    }
}
