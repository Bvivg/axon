import Foundation
import Testing
@testable import Axon

@MainActor
final class RouteRecorder {
    var routes: [Route] = []
}

@MainActor
struct LobbyViewModelTests {
    private func lobby(_ world: FakeWorld) -> (LobbyViewModel, RouteRecorder) {
        let recorder = RouteRecorder()
        let model = LobbyViewModel(services: world.services, me: Fixtures.me) { recorder.routes.append($0) }
        return (model, recorder)
    }

    @Test func refreshSplitsGamesAndLoadsNames() async {
        let world = FakeWorld()
        world.game.mine = [
            Fixtures.session(id: "a", updated: 1),
            Fixtures.session(id: "b", status: .won, turn: nil, winner: Fixtures.me, updated: 3),
            Fixtures.session(id: "c", updated: 2),
        ]
        world.game.live = [
            Fixtures.session(id: "c", updated: 2),
            Fixtures.session(id: "d", players: [Fixtures.rival, Fixtures.third], updated: 4),
        ]
        world.auth.profileMap = [Fixtures.rival: Fixtures.profile(Fixtures.rival, "grace")]
        let (model, _) = lobby(world)
        await model.refresh()
        #expect(model.active.map(\.id) == ["c", "a"])
        #expect(model.finished.map(\.id) == ["b"])
        #expect(model.live.map(\.id) == ["d"])
        #expect(model.opponentName(in: model.active[0]) == "grace")
        #expect(model.error == nil)
    }

    @Test func createOpensTheNewGame() async {
        let world = FakeWorld()
        world.game.created = Fixtures.session(id: "new", players: [Fixtures.me], status: .waiting, turn: nil)
        let (model, recorder) = lobby(world)
        await model.create(.chess)
        #expect(recorder.routes == [.game("new")])
        #expect(model.busy == nil)
    }

    @Test(arguments: [
        ("44444444-4444-4444-4444-444444444444", "44444444-4444-4444-4444-444444444444"),
        ("http://axon.test/games/chess/44444444-4444-4444-4444-44444444ABCD", "44444444-4444-4444-4444-44444444abcd"),
        ("nope", nil),
    ])
    func joinAcceptsIDsAndLinks(input: String, expected: String?) async {
        let world = FakeWorld()
        world.game.joined = Fixtures.session()
        let (model, recorder) = lobby(world)
        model.joinID = input
        await model.join()
        if let expected {
            #expect(world.game.joinedIDs == [expected])
            #expect(recorder.routes == [.game(Fixtures.sessionID)])
        } else {
            #expect(world.game.joinedIDs.isEmpty)
            #expect(model.error == "Paste a game ID or link.")
        }
    }

    @Test func quickMatchFollowsTheMatchChannel() async {
        let world = FakeWorld()
        world.game.ticket = MatchTicket.with { $0.status = .searching; $0.game = "chess" }
        let (model, recorder) = lobby(world)
        await model.quickMatch(.chess)
        #expect(model.matching == .chess)
        #expect(world.channels.opened == [.match(.chess)])
        world.channels.last?.emit(.match(MatchTicket.with { $0.status = .matched; $0.sessionID = "m1" }))
        #expect(await eventually { recorder.routes == [.game("m1")] })
        #expect(model.matching == nil)
        #expect(world.channels.last?.closed == true)
    }

    @Test func quickMatchCanMatchImmediatelyAndBeCancelled() async {
        let world = FakeWorld()
        world.game.ticket = MatchTicket.with { $0.status = .matched; $0.sessionID = "m2" }
        let (model, recorder) = lobby(world)
        await model.quickMatch(.quoridor)
        #expect(recorder.routes == [.game("m2")])
        world.game.ticket = MatchTicket.with { $0.status = .searching }
        await model.quickMatch(.chess)
        await model.cancelMatch()
        #expect(world.game.cancelled == [.chess])
        #expect(model.matching == nil)
    }

    @Test func errorsAreReadable() async {
        let world = FakeWorld()
        world.game.failure = APIError(.unavailable)
        let (model, _) = lobby(world)
        await model.refresh()
        #expect(model.error == "The server is unreachable.")
    }
}

@MainActor
struct AuthViewModelTests {
    @Test func validatesBeforeSubmitting() {
        let model = AuthViewModel(auth: FakeAuth())
        #expect(!model.canSubmit)
        model.email = "ada"
        model.password = "secret"
        #expect(model.validationMessage == "Enter a valid email.")
        model.email = "ada@axon.test"
        #expect(model.canSubmit)
        model.mode = .register
        model.username = "ad"
        #expect(model.validationMessage == "Username needs at least 3 characters.")
        model.username = "ada"
        #expect(model.validationMessage == "Password needs at least 8 characters.")
        model.password = "correct horse"
        #expect(model.canSubmit)
    }

    @Test func registersWithTrimmedFields() async {
        let auth = FakeAuth()
        auth.registerResult = .success(Fixtures.user())
        let model = AuthViewModel(auth: auth)
        model.mode = .register
        model.email = "  ada@axon.test "
        model.username = " ada "
        model.password = "correct horse"
        let user = await model.submit()
        #expect(user?.id == Fixtures.me)
        #expect(auth.lastRegistration?.email == "ada@axon.test")
        #expect(auth.lastRegistration?.username == "ada")
    }

    @Test func wrongPasswordShowsAMessage() async {
        let model = AuthViewModel(auth: FakeAuth())
        model.email = "ada@axon.test"
        model.password = "wrong pass"
        #expect(await model.submit() == nil)
        #expect(model.error == "Wrong email or password.")
        #expect(!model.isSubmitting)
    }
}

@MainActor
struct SettingsViewModelTests {
    @Test func rejectsInvalidAndAppliesValidAddresses() async throws {
        var applied: [GatewayAddress] = []
        let model = SettingsViewModel(current: .fallback) { applied.append($0) }
        model.address = "localhost"
        #expect(await model.save() == false)
        #expect(model.error == "Use an http:// or https:// address.")
        model.address = "http://192.168.1.5:18080"
        #expect(model.changesAccount)
        #expect(await model.save())
        #expect(applied == [try GatewayAddress(parsing: "http://192.168.1.5:18080")])
    }
}

@MainActor
struct ReplayViewModelTests {
    private func history(plies: UInt32) -> SessionHistory {
        SessionHistory.with {
            $0.session = Fixtures.session(status: .won, turn: nil, winner: Fixtures.me, revision: plies)
            $0.frames = (0...plies).reversed().map { ply in HistoryFrame.with { $0.ply = ply } }
        }
    }

    @Test func startsAtTheEndAndNavigatesWithinBounds() async {
        let world = FakeWorld()
        world.game.historyResult = history(plies: 5)
        let model = ReplayViewModel(sessionID: Fixtures.sessionID, game: world.game)
        await model.load()
        #expect(model.positionText == "Move 5 of 5")
        #expect(model.resultText == "X won")
        model.next()
        #expect(model.index == 5)
        model.first()
        #expect(model.positionText == "Start")
        model.previous()
        #expect(model.index == 0)
        model.next()
        #expect(model.positionText == "Move 1 of 5")
        #expect(model.highlightedPly == 0)
        model.index = 99
        #expect(model.index == 5)
    }

    @Test func missingReplayShowsAnError() async {
        let model = ReplayViewModel(sessionID: Fixtures.sessionID, game: FakeGame())
        await model.load()
        #expect(model.error == "This game has no replay.")
        #expect(model.frame == nil)
    }
}

@MainActor
struct SessionStatusTextTests {
    @Test(arguments: [
        (Fixtures.session(players: [Fixtures.me], status: .waiting, turn: nil), Fixtures.me, "Waiting for an opponent"),
        (Fixtures.session(), Fixtures.me, "Your turn"),
        (Fixtures.session(turn: Fixtures.rival), Fixtures.me, "Opponent's turn"),
        (Fixtures.session(status: .draw, turn: nil), Fixtures.me, "Draw"),
        (Fixtures.session(), Fixtures.third, "X to move"),
        (Fixtures.session(game: .chess, status: .won, turn: nil, winner: Fixtures.rival), Fixtures.third, "Black won"),
        (Fixtures.session(game: .quoridor, turn: Fixtures.rival), Fixtures.third, "Green to move"),
    ])
    func headlines(session: Session, me: String, text: String) {
        #expect(SessionStatusText.headline(session, me: me) == text)
    }

    @Test func detailsExplainHowAGameEnded() {
        var session = Fixtures.session(status: .won, turn: nil, winner: Fixtures.me)
        session.timedOut = true
        #expect(SessionStatusText.detail(session) == "on time")
        var mate = Fixtures.session(game: .chess, status: .won, turn: nil, winner: Fixtures.me)
        mate.chess.end = .checkmate
        #expect(SessionStatusText.detail(mate) == "by checkmate")
        #expect(SessionStatusText.detail(Fixtures.session()) == nil)
    }
}

@MainActor
struct AppModelTests {
    private func app(_ world: FakeWorld) -> (AppModel, Counter) {
        let counter = Counter()
        let settings = GatewaySettings(storage: MemoryStorage())
        let vault = TokenVault(store: InMemoryTokenStore())
        let model = AppModel(settings: settings, vault: vault) { _, _ in
            counter.value += 1
            return world.services
        }
        return (model, counter)
    }

    @Test func restoresTheSessionOrAsksToSignIn() async {
        let world = FakeWorld()
        let (signedOut, _) = app(world)
        await signedOut.start()
        #expect(signedOut.phase == .signedOut)
        world.auth.restored = Fixtures.user()
        let (signedIn, _) = app(world)
        await signedIn.start()
        #expect(signedIn.user?.id == Fixtures.me)
    }

    @Test func expiredSessionsSignOut() async {
        let world = FakeWorld()
        world.auth.restored = Fixtures.user()
        let (model, _) = app(world)
        await model.start()
        model.path = [.game("x")]
        world.auth.signOuts.yield()
        #expect(await eventually { model.phase == .signedOut })
        #expect(model.path.isEmpty)
    }

    @Test func changingTheGatewayRebuildsServicesAndSignsOut() async throws {
        let world = FakeWorld()
        world.auth.restored = Fixtures.user()
        let (model, counter) = app(world)
        await model.start()
        try await model.updateGateway(GatewayAddress(parsing: "http://10.0.0.2:18080"))
        #expect(counter.value == 2)
        #expect(world.auth.logoutCount == 1)
        #expect(model.phase == .signedOut)
        #expect(model.settings.address.host == "http://10.0.0.2:18080")
    }
}

@MainActor
final class Counter {
    var value = 0
}

final class MemoryStorage: SettingsStorage, @unchecked Sendable {
    private var values: [String: String] = [:]

    func string(forKey key: String) -> String? { values[key] }
    func set(_ value: String?, forKey key: String) { values[key] = value }
}
