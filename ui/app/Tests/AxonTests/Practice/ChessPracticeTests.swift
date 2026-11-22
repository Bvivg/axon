import Foundation
import Testing
@testable import Axon

struct ChessPracticeTests {
    private struct SANCase {
        let fen: String
        let uci: String
        let san: String
    }

    private struct EndCase {
        let name: String
        let fen: String
        let moves: [String]
        let outcome: PracticeOutcome?
        let end: ChessPractice.End?

        init(_ name: String, _ fen: String, _ moves: [String], _ outcome: PracticeOutcome?, _ end: ChessPractice.End?) {
            self.name = name
            self.fen = fen
            self.moves = moves
            self.outcome = outcome
            self.end = end
        }
    }

    private static let endCases = [
        EndCase("fool's mate", ChessPractice.startFEN, ["f2f3", "e7e5", "g2g4", "d8h4"], .won(seat: 1), .checkmate),
        EndCase(
            "scholar's mate", ChessPractice.startFEN, ["e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7"], .won(seat: 0), .checkmate
        ),
        EndCase("no stalemate yet", "7k/5Q2/6K1/8/8/8/8/8 w - - 0 1", ["f7f6"], nil, nil),
        EndCase("stalemate by queen", "k7/8/1K6/8/8/8/8/2Q5 w - - 0 1", ["c1c7"], .draw, .stalemate),
        EndCase("king takes the last piece", "8/8/8/8/8/k7/2q5/2K5 w - - 0 1", ["c1c2"], .draw, .insufficientMaterial),
        EndCase("king and knight left", "k6n/8/8/8/8/8/8/2rK4 w - - 0 1", ["d1c1"], .draw, .insufficientMaterial),
        EndCase("same-colour bishops", "k1b5/8/8/8/8/8/8/2rKB3 w - - 0 1", ["d1c1"], nil, nil),
        EndCase("two knights can still mate", "k5nn/8/8/8/8/8/8/2rK4 w - - 0 1", ["d1c1"], nil, nil),
        EndCase("fifty-move rule", "7k/8/8/8/8/8/8/R6K w - - 99 80", ["a1a2"], .draw, .fiftyMoves),
        EndCase("fifty moves reset by pawn move", "7k/8/8/8/8/8/P7/R6K w - - 99 80", ["a2a3"], nil, nil),
        EndCase(
            "threefold repetition", ChessPractice.startFEN, ["g1f3", "g8f6", "f3g1", "f6g8", "g1f3", "g8f6", "f3g1", "f6g8"],
            .draw, .threefoldRepetition
        ),
        EndCase("en passant", "rnbqkbnr/pppppppp/8/4P3/8/8/PPPP1PPP/RNBQKBNR b KQkq - 0 2", ["d7d5", "e5d6"], nil, nil),
        EndCase("promotion to knight with check", "8/5P1k/8/8/8/8/p7/K7 w - - 0 1", ["f7f8n"], nil, nil),
    ]

    private static let kiwipete = "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1"

    private func position(_ fen: String) throws -> ChessPractice.Position {
        try #require(ChessPractice.Position(fen: fen))
    }

    private func bot(_ fen: String, _ level: PracticeLevel) throws -> String? {
        var random = ConstantRandom(0.5)
        return ChessPractice.botMove(try position(fen), level: level, using: &random)
    }

    @Test func perftFromTheStartPosition() throws {
        let board = try #require(ChessPractice.Board(fen: ChessPractice.startFEN))
        #expect((1...3).map { board.perft($0) } == [20, 400, 8902])
    }

    @Test func perftFromKiwipete() throws {
        let board = try #require(ChessPractice.Board(fen: Self.kiwipete))
        #expect((1...2).map { board.perft($0) } == [48, 2039])
    }

    @Test func perftFromTheEndgameAndPromotionSuites() throws {
        let suites: [(String, [Int])] = [
            ("8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1", [14, 191, 2812]),
            ("r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1", [6, 264]),
            ("rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8", [44, 1486]),
        ]
        for (fen, want) in suites {
            let board = try #require(ChessPractice.Board(fen: fen))
            #expect((1...want.count).map { board.perft($0) } == want, "\(fen)")
        }
    }

    @Test func roundTripsFEN() throws {
        let fens = [
            ChessPractice.startFEN,
            Self.kiwipete,
            "rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3",
            "8/8/8/8/8/8/8/k6K b - - 42 99",
        ]
        for fen in fens {
            #expect(try position(fen).fen == fen)
        }
        let malformed = [
            "",
            "8/8/8/8/8/8/8/8 w - - 0 1",
            "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR x KQkq - 0 1",
            "rnbqkbnr/ppppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
        ]
        for bad in malformed {
            #expect(ChessPractice.Position(fen: bad) == nil, "\(bad)")
        }
    }

    @Test func writesSAN() throws {
        let cases = [
            SANCase(fen: ChessPractice.startFEN, uci: "g1f3", san: "Nf3"),
            SANCase(fen: ChessPractice.startFEN, uci: "e2e4", san: "e4"),
            SANCase(fen: "rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3", uci: "e5f6", san: "exf6"),
            SANCase(fen: "r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1", uci: "e1g1", san: "O-O"),
            SANCase(fen: "r3k2r/8/8/8/8/8/8/R3K2R b KQkq - 0 1", uci: "e8c8", san: "O-O-O"),
            SANCase(fen: "7k/4P3/8/8/8/8/8/K7 w - - 0 1", uci: "e7e8q", san: "e8=Q+"),
            SANCase(fen: "7k/4P3/8/8/8/8/8/K7 w - - 0 1", uci: "e7e8n", san: "e8=N"),
            SANCase(fen: "k7/8/8/8/8/8/8/KN3N2 w - - 0 1", uci: "b1d2", san: "Nbd2"),
            SANCase(fen: "k7/8/8/1N6/8/8/8/KN6 w - - 0 1", uci: "b1c3", san: "N1c3"),
            SANCase(fen: "rnbqkbnr/pppp1ppp/8/4p3/2B1P3/5Q2/PPPP1PPP/RNB1K1NR w KQkq - 0 1", uci: "f3f7", san: "Qxf7#"),
        ]
        for item in cases {
            let next = try #require(ChessPractice.apply(item.uci, to: try position(item.fen)), "\(item.uci)")
            #expect(next.san.last == item.san, "\(item.uci)")
            #expect(next.lastMove == item.uci)
        }
    }

    @Test func endsGamesLikeTheServer() throws {
        for item in Self.endCases {
            let played = try #require(PracticeReplay.play(ChessPractice.self, item.moves, from: try position(item.fen)), "\(item.name)")
            #expect(ChessPractice.outcome(played) == item.outcome, "\(item.name)")
            #expect(played.end == item.end, "\(item.name)")
        }
    }

    @Test func showsEnPassantAndPromotionOnTheBoard() throws {
        let before = try position("rnbqkbnr/pppppppp/8/4P3/8/8/PPPP1PPP/RNBQKBNR b KQkq - 0 2")
        let passant = try #require(PracticeReplay.play(ChessPractice.self, ["d7d5", "e5d6"], from: before))
        #expect(passant.fen == "rnbqkbnr/ppp1pppp/3P4/8/8/8/PPPP1PPP/RNBQKBNR b KQkq - 0 3")
        #expect(passant.san == ["d5", "exd6"])
        #expect(passant.lastMove == "e5d6")
        let promoted = try #require(ChessPractice.apply("f7f8n", to: try position("8/5P1k/8/8/8/8/p7/K7 w - - 0 1")))
        #expect(promoted.fen == "5N2/7k/8/8/8/8/p7/K7 b - - 0 1")
        #expect(promoted.san == ["f8=N+"])
        #expect(promoted.inCheck)
    }

    @Test func rejectsIllegalAndMalformedMoves() throws {
        let start = ChessPractice.start(seed: 0)
        for move in ["e2e1", "zz99", "e2e5", "e7e5", "e2e4q", ""] {
            #expect(ChessPractice.apply(move, to: start) == nil, "\(move)")
        }
        #expect(ChessPractice.legalMoves(start).count == 20)
        #expect(ChessPractice.legalMoves(start).contains("g1f3"))
    }

    @Test(arguments: PracticeLevel.allCases)
    func botPlaysALegalMoveFromTheStart(level: PracticeLevel) throws {
        let move = try #require(try bot(ChessPractice.startFEN, level))
        #expect(ChessPractice.legalMoves(ChessPractice.start(seed: 0)).contains(move))
    }

    @Test(arguments: [PracticeLevel.medium, .hard])
    func botFindsMateInOne(level: PracticeLevel) throws {
        #expect(try bot("rnbqkbnr/pppp1ppp/8/4p3/2B1P3/5Q2/PPPP1PPP/RNB1K1NR w KQkq - 0 1", level) == "f3f7")
    }

    @Test func botTakesAFreeQueen() throws {
        #expect(try bot("4k3/8/8/3q4/8/8/8/3QK3 w - - 0 1", .medium) == "d1d5")
    }

    @Test func botPromotesWithALegalPieceLetter() throws {
        let move = try #require(try bot("7k/4P3/8/8/8/8/8/K7 w - - 0 1", .hard))
        #expect(["e7e8q", "e7e8r", "e7e8b", "e7e8n"].contains(move))
    }

    @Test func botReturnsNothingWhenTheGameIsOver() throws {
        let over = try position("rnb1kbnr/pppp1ppp/8/4p3/6Pq/5P2/PPPPP2P/RNBQKBNR w KQkq - 1 3")
        #expect(over.end == .checkmate)
        #expect(ChessPractice.outcome(over) == .won(seat: 1))
        #expect(try bot(over.fen, .easy) == nil)
    }
}
