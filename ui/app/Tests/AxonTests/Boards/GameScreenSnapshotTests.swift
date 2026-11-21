#if canImport(AppKit)
import AppKit
import SwiftUI
import Testing
@testable import Axon

@MainActor
struct GameScreenSnapshotTests {
    @Test(arguments: [NSAppearance.Name.aqua, .darkAqua])
    func rendersALiveCheckersGameInAMacWindow(appearance: NSAppearance.Name) async throws {
        let world = FakeWorld()
        var session = GameFixtures.session(game: .checkers, revision: 16)
        session.checkers = CheckersFixtures.doubleKings
        session.clock.xRemainingMs = 512_000
        session.clock.oRemainingMs = 431_000
        world.game.sessions[session.id] = session
        world.auth.profileMap = [GameFixtures.rival: GameFixtures.profile(GameFixtures.rival, "grace")]

        let size = CGSize(width: 1000, height: 680)
        let window = NSWindow(contentRect: NSRect(origin: .zero, size: size), styleMask: [.titled], backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        window.appearance = NSAppearance(named: appearance)
        let host = NSHostingView(rootView: NavigationStack {
            GameView(sessionID: session.id, services: world.services, user: GameFixtures.user(), onReplay: {})
        }
        .frame(width: size.width, height: size.height)
        .background(.background))
        window.contentView = host
        window.makeKeyAndOrderFront(nil)
        defer { window.close() }

        #expect(await eventually { world.channels.last != nil })
        let channel = try #require(world.channels.last)
        channel.emit(.state(.open))
        channel.emit(.session(session))
        try await Task.sleep(for: .milliseconds(600))
        host.layoutSubtreeIfNeeded()

        let image = try #require(host.bitmapImageRepForCachingDisplay(in: host.bounds))
        host.cacheDisplay(in: host.bounds, to: image)
        let png = try #require(image.representation(using: .png, properties: [:]))
        try FileManager.default.createDirectory(at: BoardSnapshotTests.directory, withIntermediateDirectories: true)
        let name = "checkers-game-macos-\(appearance == .darkAqua ? "dark" : "light").png"
        try png.write(to: BoardSnapshotTests.directory.appending(path: name))
        #expect(image.pixelsWide >= Int(size.width))
        #expect(world.channels.opened == [.session(session.id)])
    }
}
#endif
