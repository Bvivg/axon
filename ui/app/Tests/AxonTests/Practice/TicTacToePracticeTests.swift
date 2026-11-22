import Foundation
import Testing
@testable import Axon

struct TicTacToePracticeTests {
    private func board(_ raw: String) -> TicTacToePractice.Position {
        let marks: [Character: Int] = ["X": TicTacToePractice.x, "O": TicTacToePractice.o]
        return TicTacToePractice.Position(cells: raw.map { marks[$0] ?? TicTacToePractice.empty })
    }

    private func bot(_ position: TicTacToePractice.Position, _ level: PracticeLevel) -> String? {
        var random = ConstantRandom(0)
        return TicTacToePractice.botMove(position, level: level, using: &random)
    }

    @Test func detectsWinsAndDraws() throws {
        let start = TicTacToePractice.start(seed: 0)
        let won = try #require(PracticeReplay.play(TicTacToePractice.self, ["0", "3", "1", "4", "2"], from: start))
        #expect(TicTacToePractice.outcome(won) == .won(seat: 0))
        #expect(won.winningLine == [0, 1, 2])
        #expect(TicTacToePractice.legalMoves(won).isEmpty)
        #expect(TicTacToePractice.apply("8", to: won) == nil)
        let drawn = try #require(PracticeReplay.play(TicTacToePractice.self, ["0", "1", "2", "4", "3", "5", "7", "6", "8"], from: start))
        #expect(TicTacToePractice.outcome(drawn) == .draw)
        #expect(drawn.winningLine.isEmpty)
        #expect(drawn.lastMove == "8")
    }

    @Test func rejectsInvalidAndOccupiedMoves() throws {
        let position = try #require(TicTacToePractice.apply("0", to: TicTacToePractice.start(seed: 0)))
        #expect(position.cells[0] == TicTacToePractice.x)
        #expect(TicTacToePractice.turn(position) == 1)
        for move in ["-1", "9", "0", "1.5", "01", "", "x"] {
            #expect(TicTacToePractice.apply(move, to: position) == nil)
        }
        #expect(TicTacToePractice.legalMoves(position) == ["1", "2", "3", "4", "5", "6", "7", "8"])
    }

    @Test func blocksAndTakesWins() {
        let threat = board("XX.O.....")
        #expect(bot(threat, .hard) == "2")
        #expect(bot(threat, .easy) == "2")
        #expect(bot(board("OO.XX.X.."), .hard) == "2")
    }

    @Test func playsCrossesWithEveryLevel() {
        let position = board("XX.OO....")
        #expect(TicTacToePractice.turn(position) == 0)
        #expect(bot(position, .hard) == "2")
        #expect(bot(position, .medium) == "2")
        #expect(position.cells[2] == TicTacToePractice.empty)
    }

    @Test func hardNeverLoses() {
        func explore(_ position: TicTacToePractice.Position) {
            if let outcome = TicTacToePractice.outcome(position) {
                #expect(outcome != .won(seat: 0))
                return
            }
            for move in TicTacToePractice.legalMoves(position) {
                guard let next = TicTacToePractice.apply(move, to: position) else { continue }
                if let outcome = TicTacToePractice.outcome(next) {
                    #expect(outcome != .won(seat: 0))
                    continue
                }
                guard let reply = bot(next, .hard), let answered = TicTacToePractice.apply(reply, to: next) else {
                    Issue.record("no reply to \(next.cells)")
                    continue
                }
                explore(answered)
            }
        }
        explore(TicTacToePractice.start(seed: 0))
    }

    @Test(arguments: PracticeLevel.allCases)
    func finishesGamesAgainstItself(level: PracticeLevel) {
        for seed in UInt64(1)...5 {
            let game = PracticeReplay.selfPlay(TicTacToePractice.self, seed: seed, levels: [level, .hard])
            #expect(game.legal)
            #expect(TicTacToePractice.outcome(game.position) != nil)
            #expect(TicTacToePractice.outcome(game.position) != .won(seat: 0))
        }
    }
}
