import SwiftUI

struct GameBoard: View {
    let kind: GameKind
    let ticTacToe: TicTacToeBoardState
    let chess: ChessBoardState
    let quoridor: QuoridorBoardState
    let perspective: Int
    let interactive: Bool
    var onCell: (Int) -> Void = { _ in }
    var onMove: (String) -> Void = { _ in }

    init(session: Session, perspective: Int, interactive: Bool, onCell: @escaping (Int) -> Void, onMove: @escaping (String) -> Void) {
        kind = session.kind
        ticTacToe = session.ticTacToe
        chess = session.chess
        quoridor = session.quoridor
        self.perspective = perspective
        self.interactive = interactive
        self.onCell = onCell
        self.onMove = onMove
    }

    init(kind: GameKind, frame: HistoryFrame) {
        self.kind = kind
        ticTacToe = frame.ticTacToe
        chess = frame.chess
        quoridor = frame.quoridor
        perspective = 0
        interactive = false
    }

    var body: some View {
        switch kind {
        case .ticTacToe:
            TicTacToeBoardView(board: ticTacToe, interactive: interactive, onTap: onCell)
        case .chess:
            ChessBoardView(board: chess, perspective: perspective, interactive: interactive, onMove: onMove)
        case .quoridor:
            QuoridorBoardView(board: quoridor, perspective: perspective, interactive: interactive, onMove: onMove)
        }
    }
}

struct AdaptiveGameLayout<Board: View, Panel: View>: View {
    @ViewBuilder var board: () -> Board
    @ViewBuilder var panel: () -> Panel

    var body: some View {
        GeometryReader { proxy in
            if proxy.size.width >= 760 {
                HStack(alignment: .top, spacing: 24) {
                    board()
                        .frame(maxWidth: min(proxy.size.height - 32, proxy.size.width * 0.62))
                    ScrollView {
                        VStack(alignment: .leading, spacing: 16) { panel() }
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .frame(minWidth: 260, maxWidth: 380)
                }
                .padding(16)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            } else {
                ScrollView {
                    VStack(alignment: .leading, spacing: 16) {
                        board()
                        panel()
                    }
                    .padding(16)
                }
            }
        }
    }
}
