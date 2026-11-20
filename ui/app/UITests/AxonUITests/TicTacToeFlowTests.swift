import XCTest

final class TicTacToeFlowTests: XCTestCase {
    private enum FlowError: Error, CustomStringConvertible {
        case timedOut(String)

        var description: String {
            switch self {
            case .timedOut(let waiting): "timed out waiting for \(waiting)"
            }
        }
    }

    @MainActor
    func testRegisterPlayAgainstALiveOpponentAndReplay() async throws {
        continueAfterFailure = false
        let gateway = try E2EGateway.baseURL()
        let app = XCUIApplication()
        app.launchArguments = [
            AccessibilityID.Launch.gatewayArgument, gateway.absoluteString,
            AccessibilityID.Launch.resetArgument, "YES",
        ]
        app.launch()

        try await register(.random(prefix: "app"), in: app)

        let sessionID = try await createTicTacToe(in: app)
        let opponent = Opponent(baseURL: gateway)
        try await opponent.register()
        _ = try await opponent.joinSession(sessionID)
        let socket = try await opponent.connect(to: sessionID)
        addTeardownBlock { await socket.close() }

        try await playTopRowToWin(in: app, against: socket)
        try await replayFromTheEndToTheStart(in: app)
    }

    @MainActor
    private func register(_ account: TestAccount, in app: XCUIApplication) async throws {
        let mode = try await existing(app.descendants(matching: .any)[AccessibilityID.Auth.mode], "the auth mode switch")
        let registerMode = mode.buttons["Register"]
        if registerMode.waitForExistence(timeout: 5) {
            registerMode.tap()
        } else {
            mode.tap()
        }
        try await type(account.email, into: app.textFields[AccessibilityID.Auth.email], "the email field")
        try await type(account.username, into: app.textFields[AccessibilityID.Auth.username], "the username field")
        try await type(account.password, into: app.secureTextFields[AccessibilityID.Auth.password], "the password field")
        app.buttons[AccessibilityID.Auth.submit].tap()
        try await existing(app.descendants(matching: .any)[AccessibilityID.Lobby.root], "the lobby", timeout: 30)
        await dismissSavePasswordPrompt(in: app, within: 10)
    }

    @MainActor
    @discardableResult
    private func dismissSavePasswordPrompt(in app: XCUIApplication, within seconds: TimeInterval) async -> Bool {
        let notNow = NSPredicate(format: "label IN %@", ["Not Now", "Не сейчас"])
        let candidates = [app, XCUIApplication(bundleIdentifier: "com.apple.springboard")].map {
            $0.descendants(matching: .any).matching(notNow).firstMatch
        }
        let deadline = Date.now.addingTimeInterval(seconds)
        while Date.now < deadline {
            if let button = candidates.first(where: { $0.exists && $0.isHittable }) {
                button.tap()
                try? await Task.sleep(for: .seconds(2))
                return true
            }
            try? await Task.sleep(for: .milliseconds(300))
        }
        return false
    }

    @MainActor
    private func createTicTacToe(in app: XCUIApplication) async throws -> String {
        let create = try await existing(app.buttons[AccessibilityID.Lobby.create("tictactoe")], "the tic-tac-toe button")
        let sessionLabel = app.staticTexts[AccessibilityID.Game.sessionID]
        await dismissSavePasswordPrompt(in: app, within: 5)
        attachScreenshot("Lobby", of: app)
        for _ in 0..<3 where !sessionLabel.exists {
            await dismissSavePasswordPrompt(in: app, within: 2)
            if create.exists && create.isHittable {
                create.tap()
            }
            _ = sessionLabel.waitForExistence(timeout: 8)
        }
        try await existing(sessionLabel, "the session id")
        try await until(sessionLabel, "the session id", NSPredicate(format: "label != ''"))
        return sessionLabel.label
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

    @MainActor
    private func type(_ text: String, into field: XCUIElement, _ name: String) async throws {
        try await existing(field, name).tap()
        field.typeText(text)
    }

    @MainActor
    @discardableResult
    private func existing(
        _ element: XCUIElement,
        _ name: String,
        timeout: TimeInterval = 20
    ) async throws -> XCUIElement {
        try await until(element, name, NSPredicate(format: "exists == true"), timeout: timeout)
        return element
    }

    @MainActor
    private func label(of element: XCUIElement, is expected: String, timeout: TimeInterval = 20) async throws {
        try await existing(element, "an element labelled \(expected)", timeout: timeout)
        try await until(element, "label \"\(expected)\"", NSPredicate(format: "label == %@", expected), timeout: timeout)
    }

    @MainActor
    private func until(
        _ element: XCUIElement,
        _ waiting: String,
        _ predicate: NSPredicate,
        timeout: TimeInterval = 20
    ) async throws {
        let expectation = XCTNSPredicateExpectation(predicate: predicate, object: element)
        guard await XCTWaiter().fulfillment(of: [expectation], timeout: timeout) == .completed else {
            let seen = element.exists ? "\"\(element.label)\"" : "no element"
            let app = XCUIApplication()
            attachScreenshot("Timed out: \(waiting)", of: app)
            let tree = XCTAttachment(string: app.debugDescription)
            tree.name = "Accessibility tree"
            tree.lifetime = .keepAlways
            add(tree)
            throw FlowError.timedOut("\(waiting) (\(seen))")
        }
    }

    @MainActor
    private func attachScreenshot(_ name: String, of app: XCUIApplication) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
