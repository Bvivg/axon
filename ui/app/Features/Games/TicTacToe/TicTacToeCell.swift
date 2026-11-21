import Foundation

struct TicTacToeCell: Identifiable, Hashable, Sendable {
    let index: Int
    let mark: Mark
    let winning: Bool

    var id: Int { index }
    var row: Int { index / 3 }
    var column: Int { index % 3 }
    var isEmpty: Bool { mark != .x && mark != .o }
    var label: String { Self.label(for: mark) }

    static func label(for mark: Mark) -> String {
        switch mark {
        case .x: "X"
        case .o: "O"
        default: "Empty"
        }
    }

    static func cells(of board: TicTacToeBoardState) -> [TicTacToeCell] {
        let winning = Set(board.winningCells.map(Int.init))
        return (0..<9).map { index in
            TicTacToeCell(
                index: index,
                mark: index < board.cells.count ? board.cells[index] : .unspecified,
                winning: winning.contains(index)
            )
        }
    }

    static func winningLine(of board: TicTacToeBoardState) -> (from: Int, to: Int)? {
        let cells = board.winningCells.map(Int.init).filter { (0..<9).contains($0) }.sorted()
        guard cells.count >= 3, let first = cells.first, let last = cells.last else { return nil }
        return (first, last)
    }
}
