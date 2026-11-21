import XCTest

enum FlowError: Error, CustomStringConvertible {
    case timedOut(String)

    var description: String {
        switch self {
        case .timedOut(let waiting): "timed out waiting for \(waiting)"
        }
    }
}

extension XCTestCase {
    @MainActor
    func launchApp(gateway: URL) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = [
            AccessibilityID.Launch.gatewayArgument, gateway.absoluteString,
            AccessibilityID.Launch.resetArgument, "YES",
        ]
        app.launch()
        return app
    }

    @MainActor
    func register(_ account: TestAccount, in app: XCUIApplication) async throws {
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
    func dismissSavePasswordPrompt(in app: XCUIApplication, within seconds: TimeInterval) async -> Bool {
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
    func createGame(_ game: String, in app: XCUIApplication) async throws -> String {
        let create = try await existing(app.buttons[AccessibilityID.Lobby.create(game)], "the \(game) button")
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
    func type(_ text: String, into field: XCUIElement, _ name: String) async throws {
        try await existing(field, name).tap()
        field.typeText(text)
    }

    @MainActor
    @discardableResult
    func existing(
        _ element: XCUIElement,
        _ name: String,
        timeout: TimeInterval = 20
    ) async throws -> XCUIElement {
        try await until(element, name, NSPredicate(format: "exists == true"), timeout: timeout)
        return element
    }

    @MainActor
    func label(of element: XCUIElement, is expected: String, timeout: TimeInterval = 20) async throws {
        try await existing(element, "an element labelled \(expected)", timeout: timeout)
        try await until(element, "label \"\(expected)\"", NSPredicate(format: "label == %@", expected), timeout: timeout)
    }

    @MainActor
    func until(
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
    func attachScreenshot(_ name: String, of app: XCUIApplication) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
