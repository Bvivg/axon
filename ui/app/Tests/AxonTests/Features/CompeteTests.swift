import Foundation
import Testing
@testable import Axon

enum CompeteFixtures {
    static let id = "66666666-6666-6666-6666-666666666666"

    static func tournament(
        status: TournamentStatus = .registration,
        creator: String = GameFixtures.rival,
        players: [String] = [GameFixtures.rival],
        format: TournamentFormat = .singleElimination
    ) -> Tournament {
        Tournament.with {
            $0.id = id
            $0.name = "Friday cup"
            $0.game = GameKind.chess.rawValue
            $0.format = format
            $0.status = status
            $0.capacity = 8
            $0.totalRounds = 3
            $0.creatorID = creator
            $0.playerCount = UInt32(players.count)
            $0.registrations = players.enumerated().map { index, user in
                TournamentRegistration.with {
                    $0.userID = user
                    $0.rating = 1500
                    $0.seed = UInt32(index + 1)
                }
            }
        }
    }

    static func position(_ ply: UInt32, cp: Int32, best: String? = nil, moveClass: MoveClass = .good) -> PositionAnalysis {
        PositionAnalysis.with {
            $0.ply = ply
            $0.centipawns = cp
            $0.moveClass = moveClass
            if let best { $0.bestMove = best }
        }
    }
}

@MainActor
struct TournamentTests {
    let world = FakeWorld()

    private func model(admin: Bool = false) -> TournamentViewModel {
        var user = GameFixtures.user()
        if admin { user.roles = ["admin"] }
        return TournamentViewModel(id: CompeteFixtures.id, services: world.services, user: user, refreshInterval: .seconds(3600))
    }

    @Test func listsOnlyGamesTheAppKnows() async {
        var unknown = CompeteFixtures.tournament()
        unknown.id = "other"
        unknown.game = "go"
        world.tournaments.listed = [CompeteFixtures.tournament(), unknown]
        let model = TournamentsViewModel(api: world.tournaments)
        await model.refresh()
        #expect(model.tournaments.map(\.id) == [CompeteFixtures.id])
        #expect(model.loaded)
        #expect(model.tournaments.first?.summary == "Chess · Single elimination · 1/8 players")
    }

    @Test func creatingReusesTheRequestUntilItSucceeds() async {
        let model = TournamentsViewModel(api: world.tournaments)
        let spec = NewTournament(name: "  Cup  ", game: .chess, format: .singleElimination, capacity: 8, rounds: 4)
        world.tournaments.failure = APIError(.unavailable)
        #expect(await model.create(spec) == nil)
        #expect(model.createError == "The server is unreachable.")
        world.tournaments.failure = nil
        let created = await model.create(spec)
        #expect(created?.name == "Cup")
        #expect(world.tournaments.created.count == 2)
        #expect(world.tournaments.created[0].1 == world.tournaments.created[1].1)
        #expect(world.tournaments.created[1].0.rounds == nil)
        #expect(model.tournaments.first?.id == "t-new")
    }

    @Test func aPlayerCanRegisterButNotStartSomeoneElsesTournament() async {
        world.tournaments.stored[CompeteFixtures.id] = CompeteFixtures.tournament()
        world.auth.profileMap = [GameFixtures.rival: GameFixtures.profile(GameFixtures.rival, "grace")]
        let model = model()
        await model.refresh()
        #expect(model.canRegister)
        #expect(!model.canWithdraw)
        #expect(!model.canStart)
        #expect(!model.canCancel)
        #expect(model.name(GameFixtures.rival) == "grace")
        world.tournaments.actionResult = .success(CompeteFixtures.tournament(players: [GameFixtures.rival, GameFixtures.me]))
        await model.perform(.register)
        #expect(world.tournaments.actions.map(\.action) == [.register])
        #expect(model.isRegistered)
        #expect(model.canWithdraw)
    }

    @Test func theCreatorStartsOnlyWithTwoPlayers() async {
        world.tournaments.stored[CompeteFixtures.id] = CompeteFixtures.tournament(creator: GameFixtures.me, players: [GameFixtures.me])
        let model = model()
        await model.refresh()
        #expect(model.canStart)
        #expect(!model.startEnabled)
        #expect(model.canCancel)
        #expect(!model.overrides(.start))
    }

    @Test func anAdminOverrideAsksForConfirmationWithAReason() async {
        world.tournaments.stored[CompeteFixtures.id] = CompeteFixtures.tournament(status: .running)
        let model = model(admin: true)
        await model.refresh()
        #expect(model.canCancel)
        model.request(.cancel)
        #expect(model.confirming == .cancel)
        #expect(world.tournaments.actions.isEmpty)
        await model.perform(.cancel, reason: "Abuse")
        #expect(world.tournaments.actions.first?.reason == "Abuse")
    }

    @Test func pendingMatchesBecomeOpenableOnceTheGameExists() async {
        var tournament = CompeteFixtures.tournament(status: .running, players: [GameFixtures.me, GameFixtures.rival])
        tournament.rounds = [TournamentRound.with {
            $0.number = 1
            $0.status = .running
            $0.matches = [TournamentMatch.with {
                $0.board = 1
                $0.sessionID = GameFixtures.sessionID
                $0.playerIds = [GameFixtures.me, GameFixtures.rival]
                $0.result = .pending
            }]
        }]
        world.tournaments.stored[CompeteFixtures.id] = tournament
        let model = model()
        await model.refresh()
        let match = tournament.rounds[0].matches[0]
        #expect(!model.isReady(match))
        world.game.sessions[GameFixtures.sessionID] = GameFixtures.session()
        await model.refresh()
        #expect(model.isReady(match))
    }

    @Test func formatsRoundsAndPoints() {
        let tournament = CompeteFixtures.tournament()
        #expect(tournament.roundTitle(3) == "Final")
        #expect(tournament.roundTitle(2) == "Semifinals")
        #expect(tournament.roundTitle(1) == "Quarterfinals")
        #expect(CompeteFixtures.tournament(format: .swiss).roundTitle(3) == "Round 3")
        #expect(TournamentPoints.label(1) == "½")
        #expect(TournamentPoints.label(5) == "2½")
        #expect(TournamentPoints.label(4) == "2")
        #expect(!GameKind.tournamentGames.contains(.durak))
    }
}

@MainActor
struct AnalysisTests {
    private func history(rated: Bool = true, moves: [String] = ["e4", "e5"]) -> SessionHistory {
        SessionHistory.with {
            $0.session = GameFixtures.session(game: .chess, status: .won, turn: nil, winner: GameFixtures.me)
            $0.session.rated = rated
            $0.session.chess = ChessBoardState.with { $0.san = moves }
            $0.frames = Self.fens.enumerated().map { ply, fen in
                HistoryFrame.with {
                    $0.ply = UInt32(ply)
                    $0.chess = ChessBoardState.with { $0.fen = fen }
                }
            }
        }
    }

    static let fens = [
        "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
        "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq - 0 1",
        "rnbqkbnr/pppp1ppp/8/4p3/4P3/8/PPPP1PPP/RNBQKBNR w KQkq - 0 2",
    ]

    private func report(_ status: Axon_Analysis_V1_AnalysisStatus) -> GameAnalysis {
        GameAnalysis.with {
            $0.status = status
            $0.engine = "stockfish"
            $0.depth = 18
            $0.positions = [
                CompeteFixtures.position(0, cp: 20, best: "e2e4"),
                CompeteFixtures.position(1, cp: 30, best: "g8f6", moveClass: .best),
                CompeteFixtures.position(2, cp: 160, moveClass: .mistake),
            ]
        }
    }

    @Test func pollsUntilTheAnalysisIsReadyAndMarksMoves() async throws {
        let world = FakeWorld()
        world.game.historyResult = history()
        world.analysis.reports = [nil, report(.pending), report(.ready)]
        let model = ReplayViewModel(
            sessionID: GameFixtures.sessionID, game: world.game, analysis: world.analysis, analysisInterval: .milliseconds(1)
        )
        await model.load()
        #expect(model.analysed)
        await model.watchAnalysis()
        #expect(world.analysis.requests == 3)
        #expect(model.analysisState == .ready)
        #expect(model.moves == ["e4", "e5?"])
        let verdict = try #require(model.verdict)
        #expect(verdict.evaluation == "+1.6")
        #expect(verdict.move == "e5? · Mistake")
        #expect(verdict.best == "Best Nf6")
        model.select(ply: 0)
        #expect(model.verdict?.move == nil)
        #expect(model.verdict?.evaluation == "+0.2")
    }

    @Test func casualGamesAreNotAnalysed() async {
        let world = FakeWorld()
        world.game.historyResult = history(rated: false)
        let model = ReplayViewModel(sessionID: GameFixtures.sessionID, game: world.game, analysis: world.analysis)
        await model.load()
        #expect(!model.analysed)
        await model.watchAnalysis()
        #expect(world.analysis.requests == 0)
    }

    @Test func gamesWithoutMovesAreNotAnalysed() async {
        let world = FakeWorld()
        world.game.historyResult = history(moves: [])
        let model = ReplayViewModel(sessionID: GameFixtures.sessionID, game: world.game, analysis: world.analysis)
        await model.load()
        #expect(!model.analysed)
        await model.watchAnalysis()
        #expect(world.analysis.requests == 0)
    }

    @Test func aFailedRequestOffersARetry() async {
        let world = FakeWorld()
        world.game.historyResult = history()
        world.analysis.failure = APIError(.unavailable)
        let model = ReplayViewModel(sessionID: GameFixtures.sessionID, game: world.game, analysis: world.analysis)
        await model.load()
        await model.watchAnalysis()
        #expect(model.analysisState == .error)
        world.analysis.failure = nil
        world.analysis.reports = [report(.failed)]
        await model.retryAnalysis()
        #expect(model.analysisState == .failed)
    }

    @Test func bestMovesFallBackToSquaresWithoutAPosition() {
        #expect(AnalysisNotation.move("e7e8q", in: "") == "e7–e8=Q")
        #expect(AnalysisNotation.move("e2e4", in: Self.fens[0]) == "e4")
    }

    @Test func labelsEvaluations() {
        #expect(CompeteFixtures.position(0, cp: 0).evaluationLabel == "0.0")
        #expect(CompeteFixtures.position(0, cp: -125).evaluationLabel == "−1.3")
        var mate = CompeteFixtures.position(0, cp: 1)
        mate.mate = 3
        #expect(mate.evaluationLabel == "+M3")
        #expect(mate.whiteShare == 1)
        mate.mate = 0
        #expect(mate.evaluationLabel == "1-0")
        #expect(abs(CompeteFixtures.position(0, cp: 0).whiteShare - 0.5) < 0.0001)
    }
}
