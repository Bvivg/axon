import SwiftUI

struct CheckersBoardView: View {
    private let board: CheckersBoardState
    private let perspective: Int
    private let interactive: Bool
    private let onMove: (String) -> Void

    @State private var path: [CheckersSquare]
    @Environment(\.colorScheme) private var colorScheme

    init(board: CheckersBoardState, perspective: Int, interactive: Bool, onMove: @escaping (String) -> Void) {
        self.init(board: board, perspective: perspective, interactive: interactive, selection: [], onMove: onMove)
    }

    init(
        board: CheckersBoardState,
        perspective: Int,
        interactive: Bool,
        selection: [CheckersSquare],
        onMove: @escaping (String) -> Void
    ) {
        self.board = board
        self.perspective = perspective
        self.interactive = interactive
        self.onMove = onMove
        _path = State(initialValue: selection)
    }

    var body: some View {
        let model = CheckersBoardModel(board, interactive: interactive, path: path)
        let palette = BoardPalette.resolve(colorScheme)
        GeometryReader { proxy in
            let side = min(proxy.size.width, proxy.size.height)
            let squareSide = side / CGFloat(Checkers.size)
            VStack(spacing: 0) {
                ForEach(0..<Checkers.size, id: \.self) { row in
                    HStack(spacing: 0) {
                        ForEach(0..<Checkers.size, id: \.self) { column in
                            if let square = CheckersSquare.at(displayRow: row, displayCol: column, perspective: perspective) {
                                squareButton(model.state(of: square), model: model, side: squareSide, palette: palette)
                            } else {
                                Rectangle()
                                    .fill(palette.chessLight)
                                    .frame(width: squareSide, height: squareSide)
                                    .accessibilityHidden(true)
                            }
                        }
                    }
                }
            }
            .frame(width: side, height: side)
            .clipShape(RoundedRectangle(cornerRadius: side * 0.015, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: side * 0.015, style: .continuous)
                    .strokeBorder(.separator, lineWidth: 1)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .aspectRatio(1, contentMode: .fit)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Checkers board")
        .onChange(of: board.squares) { path = [] }
        .onChange(of: board.legalMoves) { path = [] }
        .onChange(of: interactive) { path = [] }
        .onChange(of: perspective) { path = [] }
    }

    private func squareButton(_ state: CheckersSquareState, model: CheckersBoardModel, side: CGFloat, palette: BoardPalette) -> some View {
        Button {
            tap(state.square, model: model)
        } label: {
            CheckersSquareFace(state: state, side: side, palette: palette)
        }
        .buttonStyle(CheckersSquareButtonStyle())
        .frame(width: side, height: side)
        .accessibilityLabel(state.spokenLabel)
        .accessibilityIdentifier(AccessibilityID.Game.checkersSquare(state.square.number))
        .accessibilityAddTraits(state.selected ? .isSelected : [])
        .accessibilityActions {
            ForEach(model.moves(from: state.square), id: \.self) { move in
                Button(move.spokenName) { play(move.notation) }
            }
        }
    }

    private func tap(_ square: CheckersSquare, model: CheckersBoardModel) {
        guard interactive else { return }
        switch model.tap(square) {
        case .select(let next):
            path = next
        case .move(let notation):
            play(notation)
        }
    }

    private func play(_ notation: String) {
        guard interactive else { return }
        path = []
        onMove(notation)
    }
}

private struct CheckersSquareFace: View {
    let state: CheckersSquareState
    let side: CGFloat
    let palette: BoardPalette

    var body: some View {
        ZStack {
            Rectangle().fill(palette.chessDark)
            if state.lastMove {
                Rectangle().fill(palette.chessLast)
            }
            if state.selected {
                Rectangle().fill(palette.chessMark)
            }
            if state.movable {
                Circle()
                    .strokeBorder(palette.violet.opacity(0.75), lineWidth: max(2, side * 0.05))
                    .padding(side * 0.05)
            }
            if let piece = state.piece {
                CheckersPieceView(piece: piece)
                    .frame(width: side * 0.8, height: side * 0.8)
            }
            if state.target {
                Circle()
                    .fill(palette.chessMark)
                    .frame(width: side * 0.28, height: side * 0.28)
            }
            Text(String(state.square.number))
                .font(.system(size: max(7, side * 0.18), weight: .semibold, design: .rounded))
                .foregroundStyle(palette.chessLight.opacity(0.9))
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                .padding(.leading, side * 0.06)
                .padding(.top, side * 0.03)
                .accessibilityHidden(true)
        }
        .frame(width: side, height: side)
        .contentShape(Rectangle())
    }
}

private struct CheckersSquareButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
    }
}
