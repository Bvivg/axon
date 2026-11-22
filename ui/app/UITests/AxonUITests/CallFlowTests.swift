import XCTest

final class CallFlowTests: XCTestCase {
    @MainActor
    func testDeclineAnIncomingCallThenCallBackFromTheCallCard() async throws {
        continueAfterFailure = false
        let gateway = try E2EGateway.baseURL()
        let peer = Opponent(baseURL: gateway, account: .random(prefix: "caller"))
        try await peer.register()

        let account = TestAccount.random(prefix: "callee")
        let app = launchApp(gateway: gateway)
        try await register(account, in: app)
        try await openCalls(in: app)
        let me = try await peer.profile(username: account.username).id

        let incoming = try await peer.startAudioCall(to: me)
        try await label(of: status(in: app), is: "Incoming call")
        attachScreenshot("Incoming call", of: app)
        try await existing(app.buttons[AccessibilityID.Calls.decline], "the decline button").tap()
        let declined = try await peer.endedCall(incoming.id)
        XCTAssertEqual(declined.endReason, "declined")
        try await peer.acknowledgeCleanup(declined)

        let row = try await existing(app.buttons[AccessibilityID.Calls.call(incoming.id)], "the declined call in the history")
        try await until(row, "the declined call summary", NSPredicate(format: "label CONTAINS %@", "Incoming · Declined"))
        attachScreenshot("History", of: app)

        row.tap()
        try await existing(app.descendants(matching: .any)[AccessibilityID.Calls.detail], "the call card")
        let audio = try await existing(app.buttons[AccessibilityID.Calls.detailAudio], "the audio button on the call card")
        try await until(audio, "the audio button to enable", NSPredicate(format: "isEnabled == true"))
        audio.tap()
        allowMicrophone()

        let outgoing = try await peer.ringingCall()
        XCTAssertEqual(outgoing.callerID, me)
        try await label(of: status(in: app), is: "Ringing…")
        attachScreenshot("Outgoing call", of: app)
        _ = try await peer.declineCall(outgoing.id)
        try await existing(line(containing: "Outgoing · Declined", in: app), "the declined call on the call card", timeout: 30)
        attachScreenshot("Call card", of: app)
    }

    @MainActor
    private func openCalls(in app: XCUIApplication) async throws {
        let tab = try await existing(app.tabBars.buttons[AccessibilityID.Home.tab("calls")], "the Calls tab")
        let newCall = app.buttons[AccessibilityID.Calls.newCall]
        for _ in 0..<3 where !newCall.exists {
            await dismissSavePasswordPrompt(in: app, within: 1)
            tab.tap()
            _ = newCall.waitForExistence(timeout: 5)
        }
        try await existing(newCall, "the New call button")
        try await until(newCall, "calls to become available", NSPredicate(format: "isEnabled == true"), timeout: 30)
    }

    @MainActor
    private func status(in app: XCUIApplication) -> XCUIElement {
        app.staticTexts.matching(identifier: AccessibilityID.Calls.status).firstMatch
    }

    @MainActor
    private func line(containing text: String, in app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    @MainActor
    private func allowMicrophone() {
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.buttons["Allow"]
        if allow.waitForExistence(timeout: 4) { allow.tap() }
    }
}
