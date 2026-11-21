import Foundation
import Testing
@testable import Axon

struct LiveServicesTests {
    @Test @MainActor
    func wiresTheGatewayFromSettings() throws {
        let storage = VolatileSettingsStorage()
        storage.set("https://axon.test", forKey: GatewaySettings.addressKey)
        let settings = GatewaySettings(storage: storage)
        let services = LiveServices.make(settings: settings, vault: Fixtures.vault(nil))
        let channels = try #require(services.channels as? WebSocketGameChannelFactory)
        #expect(channels.url.absoluteString == "wss://axon.test/ws/game")
        #expect(services.auth is LiveAuthAPI)
        #expect(services.game is LiveGameAPI)
    }
}

@Suite(
    .enabled(if: ProcessInfo.processInfo.environment["AXON_GATEWAY_URL"] != nil),
    .timeLimit(.minutes(1))
)
struct LiveGatewayTests {
    @Test
    func registersPlaysAndReceivesTheSessionOverTheSocket() async throws {
        let address = try GatewayAddress(parsing: ProcessInfo.processInfo.environment["AXON_GATEWAY_URL"] ?? "")
        let vault = TokenVault(store: InMemoryTokenStore())
        let services = LiveServices.make(address: address, deviceID: UUID().uuidString.lowercased(), vault: vault)
        let suffix = UUID().uuidString.lowercased()
        let user = try await services.auth.register(
            email: "app-net-\(suffix)@axon.test",
            username: "appnet_" + suffix.replacingOccurrences(of: "-", with: "").prefix(16),
            password: "app-net-\(suffix)"
        )
        #expect(await services.auth.restore()?.id == user.id)

        let session = try await services.game.createSession(.ticTacToe)
        #expect(session.playerIds.contains(user.id))
        #expect(try await services.game.mySessions().contains { $0.id == session.id })

        let channel = services.channels.open(.session(session.id))
        var received: Session?
        var states: [ChannelState] = []
        for await event in channel.events {
            switch event {
            case .session(let update):
                received = update
            case .state(let state):
                states.append(state)
            case .error(let error):
                Issue.record("socket error \(error)")
            case .match:
                break
            }
            if received != nil || states.last == .closed { break }
        }
        await channel.close()
        #expect(states.contains(.open))
        #expect(received?.id == session.id)
        #expect(received?.status == .waiting)

        await services.auth.logout()
        #expect(await vault.hasSession == false)
    }
}
