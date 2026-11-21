import Foundation
import Testing
@testable import Axon

@Suite(.timeLimit(.minutes(1)))
struct WebSocketGameChannelTests {
    let harness = ChannelHarness()

    @Test
    func subscribesToTheSessionRightAfterOpen() async {
        let socket = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(socket))
        let channel = harness.channel(vault: Fixtures.vault(Fixtures.tokens("a-1")), transport: transport)
        var reader = EventReader(channel)

        #expect(await reader.take(2) == [.state(.connecting(attempt: 0)), .state(.open)])
        #expect(socket.sent == [#"{"subscribe":"s-1"}"#])
        #expect(transport.attempts == [["axon.game.v1", "axon.bearer.a-1"]])

        socket.deliver(Fixtures.sessionFrame("s-1", revision: 3))
        socket.deliver(#"{"error":"SESSION_ERROR_STALE_MOVE"}"#)
        #expect(await reader.take(2) == [.session(Fixtures.session("s-1", revision: 3)), .error(.staleMove)])

        await channel.close()
        #expect(await reader.rest() == [.state(.closed)])
        #expect(socket.closeCode == WebSocketClosure.normal)
    }

    @Test
    func subscribesToMatchmakingWithTheGameKey() async {
        let socket = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(socket))
        let vault = Fixtures.vault(Fixtures.tokens("a-1"))
        let channel = harness.channel(.match(.quoridor), vault: vault, transport: transport)
        var reader = EventReader(channel)

        _ = await reader.until(ChannelHarness.isOpen)
        #expect(socket.sent == [#"{"subscribeMatch":"quoridor"}"#])
        socket.deliver(#"{"match":{"game":"quoridor","status":"MATCH_STATUS_SEARCHING","waitMs":1200}}"#)
        var ticket = MatchTicket()
        ticket.game = "quoridor"
        ticket.status = .searching
        ticket.waitMs = 1200
        #expect(await reader.next() == .match(ticket))
        await channel.close()
    }

    @Test
    func sendsCommandsOnlyWhileOpen() async {
        let socket = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(socket))
        let channel = harness.channel(vault: Fixtures.vault(Fixtures.tokens("a-1")), transport: transport)
        var reader = EventReader(channel)
        _ = await reader.until(ChannelHarness.isOpen)

        await channel.send(SessionCodecTests.command(SessionCodecTests.ticTacToe(ply: 0, cell: 4, revision: 0)))
        #expect(socket.sent.last == #"{"move":{"cell":4,"revision":0}}"#)

        await channel.close()
        _ = await reader.rest()
        await channel.send(SessionCodecTests.command(SessionCodecTests.undo(revision: 1)))
        #expect(socket.sent.count == 2)
    }

    @Test
    func reportsUnavailableWhenSendingWithoutAConnection() async {
        let transport = ScriptedSocketTransport()
        let channel = harness.channel(vault: Fixtures.vault(Fixtures.tokens("a-1")), transport: transport)
        var reader = EventReader(channel)
        #expect(await reader.next() == .state(.connecting(attempt: 0)))

        await channel.send(SessionCodecTests.command(SessionCodecTests.ticTacToe(ply: 1, cell: 2, revision: 1)))
        #expect(await reader.next() == .error(.unavailable))
        await channel.close()
    }

    @Test
    func closesWithoutConnectingWhenSignedOut() async {
        let transport = ScriptedSocketTransport()
        let channel = harness.channel(vault: Fixtures.vault(nil), transport: transport)
        var reader = EventReader(channel)

        #expect(await reader.rest() == [.state(.connecting(attempt: 0)), .state(.closed)])
        #expect(transport.attempts.isEmpty)
    }

    @Test
    func resyncClosesWith4000AndReconnectsImmediately() async {
        let first = ScriptedSocket()
        let second = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(first), .connect(second))
        let channel = harness.channel(vault: Fixtures.vault(Fixtures.tokens("a-1")), transport: transport)
        var reader = EventReader(channel)

        _ = await reader.until(ChannelHarness.isOpen)
        first.deliver(Fixtures.sessionFrame("s-1"))
        _ = await reader.until(ChannelHarness.isSession)

        await channel.resync()
        #expect(await reader.until(ChannelHarness.isOpen) == [.state(.connecting(attempt: 0)), .state(.open)])
        #expect(first.closeCode == WebSocketClosure.resync)
        #expect(second.sent == [#"{"subscribe":"s-1"}"#])
        #expect(harness.sleeper.delays.isEmpty)

        second.deliver(Fixtures.sessionFrame("s-1", revision: 2))
        #expect(await reader.next() == .session(Fixtures.session("s-1", revision: 2)))
        await channel.close()
    }

    @Test
    func closeStopsForGood() async {
        let socket = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(socket), .connect(ScriptedSocket()))
        let channel = harness.channel(vault: Fixtures.vault(Fixtures.tokens("a-1")), transport: transport)
        var reader = EventReader(channel)
        _ = await reader.until(ChannelHarness.isOpen)

        await channel.close()
        await channel.close()
        await channel.resync()
        #expect(await reader.rest() == [.state(.closed)])
        #expect(socket.closeCode == WebSocketClosure.normal)
        #expect(transport.attempts.count == 1)
    }

    @Test
    func closeInterruptsAPendingConnection() async {
        let transport = ScriptedSocketTransport()
        let channel = harness.channel(vault: Fixtures.vault(Fixtures.tokens("a-1")), transport: transport)
        var reader = EventReader(channel)
        #expect(await reader.next() == .state(.connecting(attempt: 0)))

        await channel.close()
        #expect(await reader.rest() == [.state(.closed)])
    }

    @Test
    func factoryOpensChannelsOnTheGatewaySocket() async {
        let socket = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(socket))
        let factory = WebSocketGameChannelFactory(
            url: Fixtures.socketURL,
            vault: Fixtures.vault(Fixtures.tokens("a-1")),
            transport: transport,
            policy: ReconnectPolicy(jitter: 0),
            sleeper: harness.sleeper
        )
        let channel = factory.open(.session("s-7"))
        var reader = EventReader(channel)
        _ = await reader.until(ChannelHarness.isOpen)
        #expect(socket.sent == [#"{"subscribe":"s-7"}"#])
        await channel.close()
    }
}
