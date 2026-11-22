import Foundation
import Testing
@testable import Axon

@MainActor
struct PracticeViewModelTests {
    private func model<Game: PracticeGame>(_ game: Game.Type) -> PracticeViewModel<Game> {
        PracticeViewModel<Game>(seed: 7, botDelay: .zero)
    }

    @Test func theComputerAnswersAndUndoTakesBackBothMoves() async {
        let model = model(TicTacToePractice.self)
        #expect(model.opponent == .computer(.easy))
        #expect(model.status == "Your move")
        model.play("4")
        #expect(model.thinking)
        #expect(await eventually { model.history.count == 3 && !model.thinking })
        #expect(model.status == "Your move")
        #expect(model.canUndo)
        model.undo()
        #expect(model.history.count == 1)
        #expect(!model.canUndo)
    }

    @Test func movesOutOfTurnOrIllegalOnesAreIgnored() {
        let model = model(TicTacToePractice.self)
        model.restart(opponent: .local)
        model.play("9")
        #expect(model.history.count == 1)
        model.play("0")
        model.play("0")
        #expect(model.history.count == 2)
        #expect(model.status == "O to move")
        #expect(model.name(0) == "X")
    }

    @Test func playingSecondLetsTheComputerOpen() async {
        let model = model(ChessPractice.self)
        model.restart(seat: 1)
        #expect(model.perspective == 1)
        #expect(await eventually(.seconds(5)) { model.history.count == 2 })
        #expect(model.turn == 1)
        #expect(!model.canUndo)
        #expect(model.session.chess.legalMoves.count == 20)
    }

    @Test func twoPlayersFinishAGameOnOneDevice() {
        let model = model(TicTacToePractice.self)
        model.restart(opponent: .local)
        for cell in ["0", "3", "1", "4", "2"] { model.play(cell) }
        #expect(model.outcome == .won(seat: 0))
        #expect(model.title == "X wins")
        #expect(model.legal.isEmpty)
        #expect(model.session.ticTacToe.winningCells == [0, 1, 2])
        model.undo()
        #expect(model.outcome == nil)
    }

    @Test func durakHidesTheComputersHand() {
        let model = model(DurakPractice.self)
        let hands = model.session.durak.hands
        #expect(hands.count == 2)
        #expect(hands[0].cards.count == 6)
        #expect(hands[1].hidden)
        #expect(hands[1].cards.isEmpty)
        #expect(hands[1].count == 6)
        #expect(DurakPractice.opponents == [.computer(.medium)])
    }
}
