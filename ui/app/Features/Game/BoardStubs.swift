import SwiftUI

struct TicTacToeBoardView: View {
    let board: TicTacToeBoardState
    let interactive: Bool
    let onTap: (Int) -> Void

    init(board: TicTacToeBoardState, interactive: Bool, onTap: @escaping (Int) -> Void) {
        self.board = board
        self.interactive = interactive
        self.onTap = onTap
    }

    var body: some View {
        Grid {
            ForEach(0..<3, id: \.self) { row in
                GridRow {
                    ForEach(0..<3, id: \.self) { column in
                        let index = row * 3 + column
                        let mark = index < board.cells.count ? board.cells[index] : .unspecified
                        Button(mark == .x ? "X" : mark == .o ? "O" : "Empty") { onTap(index) }
                            .accessibilityIdentifier(AccessibilityID.Game.ticTacToeCell(index))
                            .disabled(!interactive || mark != .unspecified)
                    }
                }
            }
        }
    }
}

struct ChessBoardView: View {
    init(board: ChessBoardState, perspective: Int, interactive: Bool, onMove: @escaping (String) -> Void) {}
    var body: some View { Text("Chess") }
}

struct QuoridorBoardView: View {
    init(board: QuoridorBoardState, perspective: Int, interactive: Bool, onMove: @escaping (String) -> Void) {}
    var body: some View { Text("Quoridor") }
}

struct MoveListView: View {
    init(moves: [String], highlightedPly: Int?) {}
    var body: some View { EmptyView() }
}

struct GameClockView: View {
    let remaining: Duration

    init(remaining: Duration, running: Bool, sampledAt: Date) {
        self.remaining = remaining
    }

    var body: some View { Text(remaining.formatted(.time(pattern: .minuteSecond))).monospacedDigit() }
}
