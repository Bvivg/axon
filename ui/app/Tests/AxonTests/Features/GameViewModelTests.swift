import Foundation
import Testing
@testable import Axon

@MainActor
struct GameViewModelTests {
    private func started(_ session: Session, me: String = Fixtures.me, timeout: Duration = .seconds(60), sleep: @escaping GameViewModel.Sleeper = { try await Task.sleep(for: $0) }) async -> (GameViewModel, FakeWorld, FakeChannel) {
        let world = FakeWorld()
        world.game.sessions[session.id] = session
        world.auth.profileMap = [Fixtures.rival: Fixtures.profile(Fixtures.rival, "grace")]
        let model = GameViewModel(sessionID: session.id, me: me, services: world.services, pendingTimeout: timeout, sleep: sleep)
        await model.start()
        let channel = world.channels.last!
        channel.emit(.state(.open))
        channel.emit(.session(session))
        _ = await eventually { model.connected }
        return (model, world, channel)
    }

    @Test func subscribesAndLoadsNames() async {
        let (model, world, _) = await started(Fixtures.session())
        #expect(world.channels.opened == [.session(Fixtures.sessionID)])
        #expect(model.connected)
        #expect(model.statusText == "Your turn")
        #expect(model.interactive)
        #expect(model.name(seat: 0) == "You")
        #expect(await eventually { model.name(seat: 1) == "grace" })
    }

    @Test func ignoresOlderRevisions() async {
        let (model, _, channel) = await started(Fixtures.session(revision: 4))
        channel.emit(.session(Fixtures.session(turn: Fixtures.rival, revision: 3)))
        channel.emit(.session(Fixtures.session(turn: Fixtures.rival, revision: 5)))
        #expect(await eventually { model.session?.revision == 5 })
        #expect(model.statusText == "Opponent's turn")
        #expect(!model.interactive)
    }

    @Test func movesCarryPlyAndRevisionAndWaitForTheServer() async throws {
        let (model, _, channel) = await started(Fixtures.session(revision: 2))
        await model.tap(cell: 4)
        #expect(model.pending)
        #expect(!model.interactive)
        let sent = try #require(channel.sent.first)
        #expect(sent.move.cell == 4)
        #expect(sent.move.ply == 2)
        #expect(sent.move.revision == 2)
        await model.tap(cell: 5)
        #expect(channel.sent.count == 1)
        channel.emit(.session(Fixtures.session(turn: Fixtures.rival, revision: 3)))
        #expect(await eventually { !model.pending })
    }

    @Test func serverErrorsClearPendingWithAMessage() async {
        let (model, _, channel) = await started(Fixtures.session())
        await model.tap(cell: 0)
        channel.emit(.error(.cellTaken))
        #expect(await eventually { !model.pending })
        #expect(model.error == "That square is already taken.")
    }

    @Test func unansweredMoveTriggersResync() async {
        let (model, _, channel) = await started(Fixtures.session(), timeout: .zero, sleep: { _ in })
        await model.tap(cell: 0)
        #expect(await eventually { channel.resyncs == 1 })
        #expect(!model.pending)
        #expect(!model.connected)
    }

    @Test func noMovesWhileDisconnected() async {
        let (model, _, channel) = await started(Fixtures.session())
        channel.emit(.state(.connecting(attempt: 1)))
        #expect(await eventually { !model.connected })
        await model.tap(cell: 0)
        #expect(channel.sent.isEmpty)
        #expect(!model.interactive)
    }

    @Test func spectatorsSeeSeatsAndCannotMove() async {
        let (model, _, channel) = await started(Fixtures.session(turn: Fixtures.rival), me: Fixtures.third)
        #expect(model.isSpectator)
        #expect(model.statusText == "O to move")
        #expect(!model.interactive)
        #expect(!model.canResign)
        await model.tap(cell: 0)
        #expect(channel.sent.isEmpty)
    }

    @Test(arguments: [
        (Fixtures.me, "You won"),
        (Fixtures.rival, "You lost"),
    ])
    func finishedGamesShowTheResult(winner: String, text: String) async {
        let (model, _, _) = await started(Fixtures.session(status: .won, turn: nil, winner: winner))
        #expect(model.statusText == text)
        #expect(model.isFinished)
        #expect(!model.canResign)
    }

    @Test func undoOnlyInUnratedTicTacToeAfterAMove() async {
        var session = Fixtures.session(revision: 1)
        session.rated = false
        let (model, _, channel) = await started(session)
        #expect(model.canUndo)
        await model.undo()
        #expect(channel.sent.first?.undo.revision == 1)
        var rated = Fixtures.session(revision: 1)
        rated.rated = true
        let (ratedModel, _, _) = await started(rated)
        #expect(!ratedModel.canUndo)
    }

    @Test func drawOffersFollowTheSession() async {
        var offered = Fixtures.session(game: .chess)
        offered.drawOfferUserID = Fixtures.rival
        let (model, _, channel) = await started(offered)
        #expect(model.drawOfferedToMe)
        #expect(!model.canOfferDraw)
        await model.draw(.accept)
        #expect(channel.sent.first?.draw.action == .accept)
        let (open, _, _) = await started(Fixtures.session(game: .chess))
        #expect(open.canOfferDraw)
        #expect(!open.drawOfferedToMe)
    }

    @Test func chessAndQuoridorMovesUseTheirCommands() async {
        let (chess, _, chessChannel) = await started(Fixtures.session(game: .chess, revision: 6))
        await chess.chessMove("e2e4")
        #expect(chessChannel.sent.first?.chessMove.uci == "e2e4")
        #expect(chessChannel.sent.first?.chessMove.ply == 6)
        let (quoridor, _, quoridorChannel) = await started(Fixtures.session(game: .quoridor))
        await quoridor.quoridorMove("e2")
        #expect(quoridorChannel.sent.first?.quoridorMove.move == "e2")
    }

    @Test func cancellingAWaitingGameLeavesTheScreen() async {
        let (model, world, _) = await started(Fixtures.session(players: [Fixtures.me], status: .waiting, turn: nil))
        #expect(model.statusText == "Waiting for an opponent")
        #expect(model.canResign)
        world.game.leaveResult = nil
        await model.resign()
        #expect(model.left)
    }

    @Test func outsidersCanJoinAWaitingGame() async {
        let (model, world, channel) = await started(Fixtures.session(players: [Fixtures.rival], status: .waiting, turn: nil), me: Fixtures.me)
        #expect(model.canJoin)
        world.game.joined = Fixtures.session(turn: Fixtures.rival, revision: 1)
        await model.join()
        #expect(world.game.joinedIDs == [Fixtures.sessionID])
        #expect(model.mySeat == 0)
        #expect(channel.resyncs == 1)
    }

    @Test func missingGamesShowAnError() async {
        let world = FakeWorld()
        let model = GameViewModel(sessionID: Fixtures.sessionID, me: Fixtures.me, services: world.services)
        await model.start()
        #expect(model.loadError == "This game does not exist or is private.")
        #expect(world.channels.opened.isEmpty)
    }
}
