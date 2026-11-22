import Foundation
import Testing
@testable import Axon

struct QuoridorPracticeTests {
    private struct PawnCase {
        let name: String
        let position: QuoridorPractice.Position
        let moves: [String]
    }

    private func at(
        _ first: String,
        _ second: String,
        walls: [String] = [],
        left: [Int] = [10, 10],
        turn: Int = 0
    ) throws -> QuoridorPractice.Position {
        try #require(QuoridorPractice.Position(pawns: [first, second], walls: walls, wallsLeft: left, turn: turn))
    }

    private func wall(_ name: String) throws -> QuoridorPractice.Wall {
        try #require(QuoridorPractice.Wall(notation: name))
    }

    private func bot(_ position: QuoridorPractice.Position, _ level: PracticeLevel) -> String? {
        var random = ConstantRandom(0.5)
        return QuoridorPractice.botMove(position, level: level, using: &random)
    }

    @Test func pawnMoves() throws {
        let cases = [
            PawnCase(name: "opening", position: try at("e1", "e9"), moves: ["d1", "f1", "e2"]),
            PawnCase(name: "straight jump", position: try at("e5", "e6"), moves: ["e4", "d5", "f5", "e7"]),
            PawnCase(
                name: "wall behind gives diagonals",
                position: try at("e5", "e6", walls: ["e6h"]),
                moves: ["e4", "d5", "f5", "d6", "f6"]
            ),
            PawnCase(name: "board edge gives diagonals", position: try at("e8", "e9"), moves: ["e7", "d8", "f8", "d9", "f9"]),
            PawnCase(
                name: "diagonal closed by a wall",
                position: try at("e5", "e6", walls: ["e6h", "d6v"]),
                moves: ["e4", "d5", "f5", "f6"]
            ),
            PawnCase(name: "sideways jump", position: try at("e5", "f5"), moves: ["e4", "d5", "g5", "e6"]),
            PawnCase(name: "wall covers the second cell", position: try at("e1", "e9", walls: ["d1h"]), moves: ["d1", "f1"]),
        ]
        for item in cases {
            #expect(QuoridorPractice.pawnMoves(item.position, seat: 0).map(QuoridorPractice.cellName) == item.moves, "\(item.name)")
        }
    }

    @Test func wallsNextToAnExistingWall() throws {
        let position = try at("e1", "e9", walls: ["e3h"])
        let cases: [(String, Bool)] = [
            ("e3h", false), ("d3h", false), ("f3h", false), ("e3v", false), ("c3h", true), ("g3h", true), ("e2v", true), ("e4v", true),
        ]
        for (name, want) in cases {
            #expect(QuoridorPractice.canPlace(position, seat: 0, try wall(name)) == want, "\(name)")
        }
    }

    @Test func neverSealsAPath() throws {
        let sealed = try at("e1", "e9", walls: ["a1h", "c1h", "e1h", "g1h"])
        #expect(QuoridorPractice.canPlace(sealed, seat: 0, try wall("h2v")))
        #expect(!QuoridorPractice.canPlace(sealed, seat: 0, try wall("h1v")))
        #expect(!QuoridorPractice.legalMoves(sealed).contains("h1v"))
        #expect(!QuoridorPractice.canPlace(try at("e1", "a9", walls: ["a8h"]), seat: 0, try wall("b8v")))
    }

    @Test func measuresDistancesLikeTheServer() throws {
        let start = QuoridorPractice.start(seed: 0)
        #expect(QuoridorPractice.distance(start, from: start.pawns[0], goal: QuoridorPractice.goalRow(0)) == 8)
        let walled = try at("e1", "e9", walls: ["d1h"])
        #expect(QuoridorPractice.distance(walled, from: walled.pawns[0], goal: QuoridorPractice.goalRow(0)) == 9)
        let maze = try at("e1", "e9", walls: ["a1h", "c1h", "e1h", "g1h"])
        #expect(QuoridorPractice.distance(maze, from: maze.pawns[0], goal: QuoridorPractice.goalRow(0)) == 12)
    }

    @Test func countsEveryOpeningMove() {
        let start = QuoridorPractice.start(seed: 0)
        #expect(start.pawnNames == ["e1", "e9"])
        #expect(start.wallsLeft == [10, 10])
        #expect(QuoridorPractice.legalMoves(start).count == 3 + 128)
    }

    @Test func spendsWallsPassesTheTurnAndFindsTheWinner() throws {
        let next = try #require(QuoridorPractice.apply("e8h", to: QuoridorPractice.start(seed: 0)))
        #expect(next.wallsLeft == [9, 10])
        #expect(QuoridorPractice.turn(next) == 1)
        #expect(next.walls == ["e8h"])
        #expect(next.lastMove == "e8h")
        #expect(QuoridorPractice.apply("e8", to: next) == nil)
        let won = try #require(QuoridorPractice.apply("e9", to: try at("e8", "a9")))
        #expect(QuoridorPractice.outcome(won) == .won(seat: 0))
        #expect(QuoridorPractice.legalMoves(won).isEmpty)
        let lost = try #require(QuoridorPractice.apply("c1", to: try at("a5", "c2", turn: 1)))
        #expect(QuoridorPractice.outcome(lost) == .won(seat: 1))
    }

    @Test func keepsWallsSortedLikeTheServer() throws {
        let start = QuoridorPractice.start(seed: 0)
        let position = try #require(PracticeReplay.play(QuoridorPractice.self, ["e8h", "a1v", "c3h"], from: start))
        #expect(position.walls == ["a1v", "c3h", "e8h"])
        #expect(position.moves == ["e8h", "a1v", "c3h"])
    }

    @Test func refusesWallsOnceTheyRunOut() throws {
        let empty = try at("e1", "e9", left: [0, 10])
        #expect(!QuoridorPractice.canPlace(empty, seat: 0, try wall("a1h")))
        #expect(QuoridorPractice.legalMoves(empty) == ["d1", "f1", "e2"])
        #expect(QuoridorPractice.apply("a1h", to: empty) == nil)
    }

    @Test(arguments: PracticeLevel.allCases)
    func botPlaysALegalOpeningMove(level: PracticeLevel) throws {
        let start = QuoridorPractice.start(seed: 0)
        let move = try #require(bot(start, level))
        #expect(QuoridorPractice.legalMoves(start).contains(move))
    }

    @Test func botStepsOntoTheGoalRowWhenItCan() throws {
        #expect(bot(try at("e8", "a5"), .medium) == "e9")
    }

    @Test func botBlocksAnOpponentOneStepFromWinning() throws {
        let move = bot(try at("e5", "e2"), .medium)
        #expect(move == "d1h" || move == "e1h")
    }

    @Test func botFinishesAGameAgainstItself() {
        let game = PracticeReplay.selfPlay(QuoridorPractice.self, seed: 3, levels: [.easy, .medium])
        #expect(game.legal)
        #expect(QuoridorPractice.outcome(game.position) != nil)
    }
}
