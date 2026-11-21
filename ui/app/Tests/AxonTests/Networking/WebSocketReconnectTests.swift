import Foundation
import Testing
@testable import Axon

@Suite(.timeLimit(.minutes(1)))
struct WebSocketReconnectTests {
    let harness = ChannelHarness()

    @Test
    func reconnectsAfterDropsWithGrowingDelaysAndResetsAfterSync() async {
        let first = ScriptedSocket()
        let second = ScriptedSocket()
        let third = ScriptedSocket()
        let transport = ScriptedSocketTransport(
            .connect(first), .fail(.failed), .fail(.failed), .connect(second), .connect(third)
        )
        let channel = harness.channel(vault: Fixtures.vault(Fixtures.tokens("a-1")), transport: transport)
        var reader = EventReader(channel)

        _ = await reader.until(ChannelHarness.isOpen)
        first.deliver(Fixtures.sessionFrame("s-1"))
        _ = await reader.until(ChannelHarness.isSession)
        first.drop(code: nil)

        #expect(await reader.until(ChannelHarness.isOpen) == [
            .state(.connecting(attempt: 1)),
            .state(.connecting(attempt: 2)),
            .state(.connecting(attempt: 3)),
            .state(.open),
        ])
        #expect(harness.sleeper.delays == [.seconds(1), .seconds(2), .seconds(4)])
        #expect(second.sent == [#"{"subscribe":"s-1"}"#])

        second.deliver(Fixtures.sessionFrame("s-1", revision: 1))
        _ = await reader.until(ChannelHarness.isSession)
        second.drop(code: 1006)
        #expect(await reader.until(ChannelHarness.isOpen) == [.state(.connecting(attempt: 1)), .state(.open)])
        #expect(harness.sleeper.delays == [.seconds(1), .seconds(2), .seconds(4), .seconds(1)])
        #expect(third.sent == [#"{"subscribe":"s-1"}"#])
        await channel.close()
    }

    @Test
    func keepsBackingOffWhenTheServerClosesBeforeTheFirstState() async {
        let first = ScriptedSocket()
        let second = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(first), .connect(second))
        let channel = harness.channel(vault: Fixtures.vault(Fixtures.tokens("a-1")), transport: transport)
        var reader = EventReader(channel)

        _ = await reader.until(ChannelHarness.isOpen)
        first.deliver(#"{"error":"SESSION_ERROR_NOT_FOUND"}"#)
        #expect(await reader.next() == .error(.notFound))
        first.drop(code: 1000)
        _ = await reader.until(ChannelHarness.isOpen)
        second.drop(code: 1000)
        #expect(await reader.next() == .state(.connecting(attempt: 2)))
        #expect(harness.sleeper.delays == [.seconds(1), .seconds(2)])
        await channel.close()
    }

    @Test
    func givesUpAfterTheLastAttempt() async {
        let transport = ScriptedSocketTransport(.fail(.failed), .fail(.failed), .fail(.failed), .fail(.failed))
        let policy = ReconnectPolicy(maxAttempts: 3, jitter: 0)
        let vault = Fixtures.vault(Fixtures.tokens("a-1"))
        let channel = harness.channel(vault: vault, transport: transport, policy: policy)
        var reader = EventReader(channel)

        #expect(await reader.rest() == [
            .state(.connecting(attempt: 0)),
            .state(.connecting(attempt: 1)),
            .state(.connecting(attempt: 2)),
            .state(.connecting(attempt: 3)),
            .state(.closed),
        ])
        #expect(harness.sleeper.delays == [.seconds(1), .seconds(2), .seconds(4)])
        #expect(transport.attempts.count == 4)
    }

    @Test
    func policyViolationRefreshesTheTokenAndReconnectsWithIt() async {
        let first = ScriptedSocket()
        let second = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(first), .connect(second))
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2")))
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        let channel = harness.channel(vault: vault, transport: transport)
        var reader = EventReader(channel)

        _ = await reader.until(ChannelHarness.isOpen)
        first.drop(code: WebSocketClosure.policyViolation)
        #expect(await reader.until(ChannelHarness.isOpen) == [.state(.connecting(attempt: 0)), .state(.open)])
        #expect(transport.bearers == ["a-1", "a-2"])
        #expect(refresher.calls == ["refresh-a-1"])
        #expect(harness.sleeper.delays.isEmpty)
        #expect(second.sent == [#"{"subscribe":"s-1"}"#])
        #expect(await vault.accessToken == "a-2")
        await channel.close()
    }

    @Test
    func rejectedHandshakeRefreshesTheTokenAndReconnects() async {
        let socket = ScriptedSocket()
        let transport = ScriptedSocketTransport(.fail(.unauthorized), .connect(socket))
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2")))
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        let channel = harness.channel(vault: vault, transport: transport)
        var reader = EventReader(channel)

        _ = await reader.until(ChannelHarness.isOpen)
        #expect(transport.bearers == ["a-1", "a-2"])
        #expect(refresher.calls.count == 1)
        #expect(harness.sleeper.delays.isEmpty)
        await channel.close()
    }

    @Test
    func repeatedPolicyViolationsFallBackToBackoff() async {
        let sockets = [ScriptedSocket(), ScriptedSocket(), ScriptedSocket()]
        let transport = ScriptedSocketTransport(.connect(sockets[0]), .connect(sockets[1]), .connect(sockets[2]))
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2")), .rotated(Fixtures.tokens("a-3")))
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        let channel = harness.channel(vault: vault, transport: transport)
        var reader = EventReader(channel)

        _ = await reader.until(ChannelHarness.isOpen)
        sockets[0].drop(code: WebSocketClosure.policyViolation)
        _ = await reader.until(ChannelHarness.isOpen)
        sockets[1].drop(code: WebSocketClosure.policyViolation)
        _ = await reader.until(ChannelHarness.isOpen)
        #expect(transport.bearers == ["a-1", "a-2", "a-3"])
        #expect(harness.sleeper.delays == [.seconds(1)])
        await channel.close()
    }

    @Test
    func rejectedRefreshClosesTheChannelForGood() async {
        let socket = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(socket), .connect(ScriptedSocket()))
        let refresher = CountingRefresher(.rejected)
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        let channel = harness.channel(vault: vault, transport: transport)
        var reader = EventReader(channel)

        _ = await reader.until(ChannelHarness.isOpen)
        socket.drop(code: WebSocketClosure.policyViolation)
        #expect(await reader.rest() == [.state(.closed)])
        #expect(transport.attempts.count == 1)
        #expect(await vault.hasSession == false)
        #expect(harness.sleeper.delays.isEmpty)
    }

    @Test
    func unavailableRefreshBacksOffAndRetries() async {
        let first = ScriptedSocket()
        let second = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(first), .connect(second))
        let refresher = CountingRefresher(.unavailable)
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        let channel = harness.channel(vault: vault, transport: transport)
        var reader = EventReader(channel)

        _ = await reader.until(ChannelHarness.isOpen)
        first.drop(code: WebSocketClosure.policyViolation)
        _ = await reader.until(ChannelHarness.isOpen)
        #expect(harness.sleeper.delays == [.seconds(1)])
        #expect(transport.bearers == ["a-1", "a-1"])
        await channel.close()
    }
}
