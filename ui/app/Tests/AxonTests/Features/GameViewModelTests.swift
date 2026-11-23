import Foundation
import Testing
@testable import Axon

@MainActor
struct GameViewModelTests {
    private func started(
        _ session: Session,
        me: String = GameFixtures.me,
        timeout: Duration = .seconds(60),
        sleep: @escaping GameViewModel.Sleeper = { try await Task.sleep(for: $0) }
    ) async -> GameHarness {
        let world = FakeWorld()
        world.game.sessions[session.id] = session
        world.auth.profileMap = [GameFixtures.rival: GameFixtures.profile(GameFixtures.rival, "grace")]
        let model = GameViewModel(sessionID: session.id, me: me, services: world.services, pendingTimeout: timeout, sleep: sleep)
        await model.start()
        let channel = world.channels.last!
        channel.emit(.state(.open))
        channel.emit(.session(session))
        _ = await eventually { model.connected }
        return GameHarness(model: model, world: world, channel: channel)
    }

    @Test func subscribesAndLoadsNames() async {
        let harness1 = await started(GameFixtures.session())
        let model = harness1.model
        let world = harness1.world
        #expect(world.channels.opened == [.session(GameFixtures.sessionID)])
        #expect(model.connected)
        #expect(model.statusText == "Your turn")
        #expect(model.interactive)
        #expect(model.name(seat: 0) == "You")
        #expect(await eventually { model.name(seat: 1) == "grace" })
    }

    @Test func ignoresOlderRevisions() async {
        let harness2 = await started(GameFixtures.session(revision: 4))
        let model = harness2.model
        let channel = harness2.channel
        channel.emit(.session(GameFixtures.session(turn: GameFixtures.rival, revision: 3)))
        channel.emit(.session(GameFixtures.session(turn: GameFixtures.rival, revision: 5)))
        #expect(await eventually { model.session?.revision == 5 })
        #expect(model.statusText == "Opponent's turn")
        #expect(!model.interactive)
    }

    @Test func movesCarryPlyAndRevisionAndWaitForTheServer() async throws {
        let harness3 = await started(GameFixtures.session(revision: 2))
        let model = harness3.model
        let channel = harness3.channel
        await model.tap(cell: 4)
        #expect(model.pending)
        #expect(!model.interactive)
        let sent = try #require(channel.sent.first)
        #expect(sent.move.cell == 4)
        #expect(sent.move.ply == 2)
        #expect(sent.move.revision == 2)
        await model.tap(cell: 5)
        #expect(channel.sent.count == 1)
        channel.emit(.session(GameFixtures.session(turn: GameFixtures.rival, revision: 3)))
        #expect(await eventually { !model.pending })
    }

    @Test func serverErrorsClearPendingWithAMessage() async {
        let harness4 = await started(GameFixtures.session())
        let model = harness4.model
        let channel = harness4.channel
        await model.tap(cell: 0)
        channel.emit(.error(.cellTaken))
        #expect(await eventually { !model.pending })
        #expect(model.error == "That square is already taken.")
    }

    @Test func unansweredMoveTriggersResync() async {
        let harness5 = await started(GameFixtures.session(), timeout: .zero, sleep: { _ in })
        let model = harness5.model
        let channel = harness5.channel
        await model.tap(cell: 0)
        #expect(await eventually { channel.resyncs == 1 })
        #expect(!model.pending)
        #expect(!model.connected)
    }

    @Test func noMovesWhileDisconnected() async {
        let harness6 = await started(GameFixtures.session())
        let model = harness6.model
        let channel = harness6.channel
        channel.emit(.state(.connecting(attempt: 1)))
        #expect(await eventually { !model.connected })
        await model.tap(cell: 0)
        #expect(channel.sent.isEmpty)
        #expect(!model.interactive)
    }

    @Test func spectatorsSeeSeatsAndCannotMove() async {
        let harness7 = await started(GameFixtures.session(turn: GameFixtures.rival), me: GameFixtures.third)
        let model = harness7.model
        let channel = harness7.channel
        #expect(model.isSpectator)
        #expect(model.statusText == "O to move")
        #expect(!model.interactive)
        #expect(!model.canResign)
        await model.tap(cell: 0)
        #expect(channel.sent.isEmpty)
    }

    @Test(arguments: [
        (GameFixtures.me, "You won"),
        (GameFixtures.rival, "You lost"),
    ])
    func finishedGamesShowTheResult(winner: String, text: String) async {
        let harness8 = await started(GameFixtures.session(status: .won, turn: nil, winner: winner))
        let model = harness8.model
        #expect(model.statusText == text)
        #expect(model.isFinished)
        #expect(!model.canResign)
    }

    @Test func undoOnlyInUnratedTicTacToeAfterAMove() async {
        var session = GameFixtures.session(revision: 1)
        session.rated = false
        let harness9 = await started(session)
        let model = harness9.model
        let channel = harness9.channel
        #expect(model.canUndo)
        await model.undo()
        #expect(channel.sent.first?.undo.revision == 1)
        var rated = GameFixtures.session(revision: 1)
        rated.rated = true
        let harness10 = await started(rated)
        let ratedModel = harness10.model
        #expect(!ratedModel.canUndo)
    }

    @Test func drawOffersFollowTheSession() async {
        var offered = GameFixtures.session(game: .chess)
        offered.drawOfferUserID = GameFixtures.rival
        let harness11 = await started(offered)
        let model = harness11.model
        let channel = harness11.channel
        #expect(model.drawOfferedToMe)
        #expect(!model.canOfferDraw)
        await model.draw(.accept)
        #expect(channel.sent.first?.draw.action == .accept)
        let harness12 = await started(GameFixtures.session(game: .chess))
        let open = harness12.model
        #expect(open.canOfferDraw)
        #expect(!open.drawOfferedToMe)
    }

    @Test func chessAndQuoridorMovesUseTheirCommands() async {
        let harness13 = await started(GameFixtures.session(game: .chess, revision: 6))
        let chess = harness13.model
        let chessChannel = harness13.channel
        await chess.chessMove("e2e4")
        #expect(chessChannel.sent.first?.chessMove.uci == "e2e4")
        #expect(chessChannel.sent.first?.chessMove.ply == 6)
        let harness14 = await started(GameFixtures.session(game: .quoridor))
        let quoridor = harness14.model
        let quoridorChannel = harness14.channel
        await quoridor.quoridorMove("e2")
        #expect(quoridorChannel.sent.first?.quoridorMove.move == "e2")
    }

    @Test func checkersMovesUseTheirCommandAndShowHints() async throws {
        var session = GameFixtures.session(game: .checkers, revision: 3)
        session.checkers = CheckersFixtures.doubleKings
        let harness = await started(session)
        let model = harness.model
        #expect(model.moves == CheckersFixtures.doubleKings.moves)
        #expect(model.detailText == "You must capture")
        #expect(model.canOfferDraw)
        #expect(!model.canUndo)
        await model.play("31x22x29")
        let sent = try #require(harness.channel.sent.first)
        #expect(sent.checkersMove.move == "31x22x29")
        #expect(sent.checkersMove.ply == 3)
        #expect(sent.checkersMove.revision == 3)
        #expect(model.pending)
        var quiet = GameFixtures.session(game: .checkers, turn: GameFixtures.rival, revision: 4)
        quiet.checkers = CheckersFixtures.doubleKings
        harness.channel.emit(.session(quiet))
        #expect(await eventually { !model.pending })
        #expect(model.detailText == nil)
        #expect(model.statusText == "Opponent's turn")
    }

    @Test func cancellingAWaitingGameLeavesTheScreen() async {
        let harness15 = await started(GameFixtures.session(players: [GameFixtures.me], status: .waiting, turn: nil))
        let model = harness15.model
        let world = harness15.world
        #expect(model.statusText == "Waiting for an opponent")
        #expect(model.canResign)
        world.game.leaveResult = nil
        await model.resign()
        #expect(model.left)
    }

    @Test func outsidersCanJoinAWaitingGame() async {
        let waiting = GameFixtures.session(players: [GameFixtures.rival], status: .waiting, turn: nil)
        let harness16 = await started(waiting, me: GameFixtures.me)
        let model = harness16.model
        let world = harness16.world
        let channel = harness16.channel
        #expect(model.canJoin)
        world.game.joined = GameFixtures.session(turn: GameFixtures.rival, revision: 1)
        await model.join()
        #expect(world.game.joinedIDs == [GameFixtures.sessionID])
        #expect(model.mySeat == 0)
        #expect(channel.resyncs == 1)
    }

    @Test func loadsEachOpponentNameOnce() async {
        let harness17 = await started(GameFixtures.session(revision: 1))
        let model = harness17.model
        let channel = harness17.channel
        channel.emit(.session(GameFixtures.session(revision: 2)))
        channel.emit(.session(GameFixtures.session(revision: 3)))
        channel.emit(.session(GameFixtures.session(revision: 4)))
        #expect(await eventually { model.session?.revision == 4 && model.name(seat: 1) == "grace" })
        let requested = harness17.world.auth.profileRequests.flatMap { $0 }.filter { $0 == GameFixtures.rival }
        #expect(requested.count == 1)
    }

    @Test func unsupportedGamesShowAnErrorAndStayOffline() async {
        let world = FakeWorld()
        var session = GameFixtures.session()
        session.game = "go"
        world.game.sessions[session.id] = session
        let model = GameViewModel(sessionID: session.id, me: GameFixtures.me, services: world.services)
        await model.start()
        #expect(model.loadError == GameViewModel.unsupportedGame)
        #expect(model.session == nil)
        #expect(world.channels.opened.isEmpty)
    }

    @Test func missingGamesShowAnError() async {
        let world = FakeWorld()
        let model = GameViewModel(sessionID: GameFixtures.sessionID, me: GameFixtures.me, services: world.services)
        await model.start()
        #expect(model.loadError == "This game does not exist or is private.")
        #expect(world.channels.opened.isEmpty)
    }
}

struct GameHarness {
    let model: GameViewModel
    let world: FakeWorld
    let channel: FakeChannel
}
