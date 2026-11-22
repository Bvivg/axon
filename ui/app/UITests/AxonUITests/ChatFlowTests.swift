import XCTest

final class ChatFlowTests: XCTestCase {
    @MainActor
    func testFindAPersonExchangeMessagesAndSeeThePreview() async throws {
        continueAfterFailure = false
        let gateway = try E2EGateway.baseURL()
        let peer = Opponent(baseURL: gateway, account: .random(prefix: "peer"))
        try await peer.register()
        let peerID = await peer.userID

        let app = launchApp(gateway: gateway)
        try await register(.random(prefix: "chat"), in: app)
        try await openChats(in: app)
        try await startChat(with: peer.account.username, id: peerID, in: app)

        let greeting = "Hello from the app"
        try await send(greeting, in: app)
        let room = try await peer.room(withLastMessage: greeting)

        let socket = try await peer.connectChat()
        addTeardownBlock { await socket.close() }
        let reply = "Hi from the peer"
        try await socket.send(reply, to: room.id)
        try await existing(message(containing: reply, in: app), "the peer's reply in the feed")
        attachScreenshot("Conversation", of: app)

        try await existing(app.navigationBars.buttons.element(boundBy: 0), "the back button").tap()
        let row = try await existing(app.buttons[AccessibilityID.Chats.room(room.id)], "the room in the chat list")
        try await until(row, "the reply as the room preview", NSPredicate(format: "label CONTAINS %@", reply))
        attachScreenshot("Chats", of: app)
    }

    @MainActor
    private func openChats(in app: XCUIApplication) async throws {
        let tab = try await existing(app.tabBars.buttons[AccessibilityID.Home.tab("chats")], "the Chats tab")
        let search = chatSearch(in: app)
        for _ in 0..<3 where !search.exists {
            await dismissSavePasswordPrompt(in: app, within: 1)
            tab.tap()
            _ = search.waitForExistence(timeout: 5)
        }
        try await existing(search, "the chat list")
    }

    @MainActor
    private func chatSearch(in app: XCUIApplication) -> XCUIElement {
        app.searchFields["Search chats and people"]
    }

    @MainActor
    private func startChat(with username: String, id: String, in app: XCUIApplication) async throws {
        let search = try await existing(chatSearch(in: app), "the chat search field")
        search.tap()
        search.typeText(username)
        try await existing(app.buttons[AccessibilityID.Chats.person(id)], "\(username) in the people results").tap()
        try await existing(app.descendants(matching: .any)[AccessibilityID.Chats.conversation], "the conversation")
    }

    @MainActor
    private func send(_ text: String, in app: XCUIApplication) async throws {
        let composer = try await existing(app.descendants(matching: .any)[AccessibilityID.Chats.composer], "the composer")
        composer.tap()
        composer.typeText(text)
        try await existing(app.buttons[AccessibilityID.Chats.send], "the send button").tap()
        try await existing(message(containing: text, in: app), "\(text) in the feed")
    }

    @MainActor
    private func message(containing text: String, in app: XCUIApplication) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }
}
