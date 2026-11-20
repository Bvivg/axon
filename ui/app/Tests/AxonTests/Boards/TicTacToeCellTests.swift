import Testing
@testable import Axon

struct TicTacToeCellTests {
    @Test(arguments: [
        (Mark.x, "X"), (.o, "O"), (.unspecified, "Empty"), (.UNRECOGNIZED(7), "Empty"),
    ])
    func mapsLabels(mark: Mark, label: String) {
        #expect(TicTacToeCell.label(for: mark) == label)
    }

    @Test func buildsNineCellsWithWinners() {
        var board = TicTacToeBoardState()
        board.cells = [.x, .o, .o, .unspecified, .x, .unspecified, .unspecified, .unspecified, .x]
        board.winningCells = [8, 0, 4]
        let cells = TicTacToeCell.cells(of: board)
        #expect(cells.count == 9)
        #expect(cells.map(\.label) == ["X", "O", "O", "Empty", "X", "Empty", "Empty", "Empty", "X"])
        #expect(cells.filter(\.winning).map(\.index) == [0, 4, 8])
        #expect(cells[3].isEmpty)
        #expect(!cells[0].isEmpty)
        let line = TicTacToeCell.winningLine(of: board)
        #expect(line?.from == 0)
        #expect(line?.to == 8)
    }

    @Test func padsShortBoards() {
        let cells = TicTacToeCell.cells(of: TicTacToeBoardState())
        #expect(cells.count == 9)
        #expect(cells.allSatisfy { $0.isEmpty && !$0.winning })
        #expect(TicTacToeCell.winningLine(of: TicTacToeBoardState()) == nil)
    }
}
