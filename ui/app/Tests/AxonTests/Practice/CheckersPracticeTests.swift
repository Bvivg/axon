import Foundation
import Testing
@testable import Axon

struct CheckersPracticeTests {
    private typealias Piece = CheckersPractice.Piece

    private func at(_ pieces: [Int: Piece], turn: Int = 0, quiet: Int = 0, ply: Int = 10) -> CheckersPractice.Position {
        var squares = Array(repeating: Piece.empty, count: CheckersPractice.squareCount)
        for (number, piece) in pieces { squares[number - 1] = piece }
        return CheckersPractice.Position(squares: squares, turn: turn, quiet: quiet, ply: ply)
    }

    private func perft(_ position: CheckersPractice.Position, _ depth: Int) -> Int {
        let moves = CheckersPractice.legalMoves(position)
        if depth == 1 { return moves.count }
        return moves.compactMap { CheckersPractice.apply($0, to: position) }.reduce(0) { $0 + perft($1, depth - 1) }
    }

    private func bot(_ position: CheckersPractice.Position) -> String? {
        var random = ConstantRandom(0.5)
        return CheckersPractice.botMove(position, level: .medium, using: &random)
    }

    private struct MoveCase {
        let name: String
        let position: CheckersPractice.Position
        let moves: [String]
    }

    private static let shortWin = [
        "9-14 23-18 14x23 26x19 11-16 30-26 16x23x30 21-17 30x21x14 31-26 10-15",
        "24-19 15x24x31 29-25 12-16 22-17 14x21x30x23 32-27 31x24 28x19x12 5-9",
    ].joined(separator: " ").split(separator: " ").map(String.init)

    @Test func countsTheSamePerftAsTheServer() {
        let start = CheckersPractice.start(seed: 0)
        #expect((1...5).map { perft(start, $0) } == [7, 49, 302, 1469, 7361])
    }

    @Test func numbersTheDarkSquaresLikeTheStandardBoard() {
        let coordinates = [0, 3, 4, 28, 31].map { [CheckersPractice.row($0), CheckersPractice.col($0)] }
        #expect(coordinates == [[0, 1], [0, 7], [1, 0], [7, 0], [7, 6]])
        #expect(CheckersPractice.square(row: 2, col: 1) == 8)
        #expect(CheckersPractice.square(row: 0, col: 0) == nil)
        #expect(CheckersPractice.start(seed: 0).board == "bbbbbbbbbbbb........wwwwwwwwwwww")
        #expect(CheckersPractice.start(seed: 0).squares.first?.rawValue == 1)
        #expect(CheckersPractice.start(seed: 0).squares.last?.rawValue == 3)
    }

    @Test func generatesTheServerMoves() {
        let cases = [
            MoveCase(
                name: "black opening", position: CheckersPractice.start(seed: 0),
                moves: ["9-13", "9-14", "10-14", "10-15", "11-15", "11-16", "12-16"]
            ),
            MoveCase(
                name: "capture is forced",
                position: at([15: .blackMan, 1: .blackMan, 18: .whiteMan, 32: .whiteMan]),
                moves: ["15x22"]
            ),
            MoveCase(
                name: "any branch of a multi-jump",
                position: at([6: .blackMan, 10: .whiteMan, 18: .whiteMan, 19: .whiteMan, 27: .whiteMan]),
                moves: ["6x15x22", "6x15x24x31"]
            ),
            MoveCase(name: "crowning ends the jump", position: at([22: .blackMan, 26: .whiteMan, 27: .whiteMan]), moves: ["22x31"]),
            MoveCase(
                name: "kings step every way",
                position: at([18: .blackKing, 1: .whiteMan]),
                moves: ["18-22", "18-23", "18-14", "18-15"]
            ),
            MoveCase(
                name: "kings capture backwards",
                position: at([18: .whiteKing, 22: .blackMan, 1: .blackMan], turn: 1),
                moves: ["18x25"]
            ),
            MoveCase(
                name: "a king may circle home",
                position: at([18: .blackKing, 15: .whiteMan, 7: .whiteMan, 6: .whiteMan, 14: .whiteMan]),
                moves: ["18x9x2x11x18", "18x11x2x9x18"]
            ),
        ]
        for item in cases {
            #expect(CheckersPractice.legalMoves(item.position).sorted() == item.moves.sorted(), "\(item.name)")
        }
    }

    @Test func parsesAndPrintsNotationWithoutAcceptingJunk() {
        for raw in ["11-15", "22x15x6", "18x11x2x9x18"] {
            #expect(CheckersPractice.Move(notation: raw)?.notation == raw)
        }
        for raw in ["", "11", "0-4", "33-29", "01-05", "11-15x19", "11-15-19", String(repeating: "1x", count: 20) + "1"] {
            #expect(CheckersPractice.Move(notation: raw) == nil, "\(raw)")
        }
    }

    @Test func playsTheShortGameToABlockadeWin() throws {
        let final = try #require(PracticeReplay.play(CheckersPractice.self, Self.shortWin, from: CheckersPractice.start(seed: 0)))
        #expect(final.board == "bbbb.bbbb..w..........B.........")
        #expect(final.end == .noMoves)
        #expect(CheckersPractice.outcome(final) == .won(seat: 0))
        #expect(final.moves == Self.shortWin)
        #expect(final.lastMove == "5-9")
        #expect(CheckersPractice.legalMoves(final).isEmpty)
    }

    @Test func drawsAfterFortyQuietMovesEachAndAtThePlyCap() throws {
        let kings = at([18: .blackKing, 1: .whiteKing], quiet: CheckersPractice.quietLimit - 1)
        let quiet = try #require(CheckersPractice.apply("18-22", to: kings))
        #expect(quiet.end == .fortyMoves)
        #expect(CheckersPractice.outcome(quiet) == .draw)
        let man = at([9: .blackMan, 1: .whiteKing], quiet: CheckersPractice.quietLimit - 1)
        let reset = try #require(CheckersPractice.apply("9-13", to: man))
        #expect(reset.quiet == 0)
        let capped = try #require(CheckersPractice.apply("9-13", to: at([9: .blackMan, 1: .whiteKing], ply: CheckersPractice.maxPlies - 1)))
        #expect(capped.end == .moveLimit)
        #expect(CheckersPractice.outcome(capped) == .draw)
    }

    @Test func ignoresIllegalMoves() {
        #expect(CheckersPractice.apply("9-5", to: CheckersPractice.start(seed: 0)) == nil)
        #expect(CheckersPractice.apply("junk", to: CheckersPractice.start(seed: 0)) == nil)
    }

    @Test func botPrefersTheLongerCaptureAndCrowning() {
        let position = at([6: .blackMan, 10: .whiteMan, 18: .whiteMan, 19: .whiteMan, 27: .whiteMan])
        #expect(bot(position) == "6x15x24x31")
    }

    @Test func botDoesNotHangAPieceWhenASafeMoveExists() {
        let position = at([1: .blackMan, 11: .blackMan, 19: .whiteMan, 32: .whiteMan])
        #expect(["1-5", "1-6"].contains(bot(position) ?? ""))
    }

    @Test func botFinishesGamesAgainstItself() {
        for seed in UInt64(1)...5 {
            let game = PracticeReplay.selfPlay(CheckersPractice.self, seed: seed, levels: [.medium, .medium])
            #expect(game.legal)
            #expect(CheckersPractice.outcome(game.position) != nil)
            #expect(game.position.ply <= CheckersPractice.maxPlies)
        }
    }
}
