import SwiftUI

struct ChessBoardView: View {
    private let board: ChessBoardState
    private let perspective: Int
    private let interactive: Bool
    private let onMove: (String) -> Void

    @State private var selected: ChessSquare?
    @State private var promotion: ChessPromotionRequest?
    @Environment(\.colorScheme) private var colorScheme

    init(board: ChessBoardState, perspective: Int, interactive: Bool, onMove: @escaping (String) -> Void) {
        self.init(board: board, perspective: perspective, interactive: interactive, selection: nil, promotion: nil, onMove: onMove)
    }

    init(
        board: ChessBoardState,
        perspective: Int,
        interactive: Bool,
        selection: ChessSquare?,
        promotion: ChessPromotionRequest?,
        onMove: @escaping (String) -> Void
    ) {
        self.board = board
        self.perspective = perspective
        self.interactive = interactive
        self.onMove = onMove
        _selected = State(initialValue: selection)
        _promotion = State(initialValue: promotion)
    }

    var body: some View {
        let position = ChessPosition(fen: board.fen)
        let legal = ChessLegalMoves(interactive ? board.legalMoves : [])
        let palette = BoardPalette.resolve(colorScheme)
        let activeSelection = interactive ? selected : nil
        let targets = activeSelection.map { legal.targets(from: $0) } ?? []
        let lastMove = board.hasLastMove ? Set(ChessUCIMove.squares(of: board.lastMove)) : []
        let checkedKing = board.check ? position.king(of: position.sideToMove) : nil
        GeometryReader { proxy in
            let side = min(proxy.size.width, proxy.size.height)
            let squareSide = side / 8
            ZStack {
                VStack(spacing: 0) {
                    ForEach(0..<8, id: \.self) { row in
                        HStack(spacing: 0) {
                            ForEach(0..<8, id: \.self) { column in
                                if let square = ChessSquare.at(row: row, column: column, perspective: perspective) {
                                    squareButton(
                                        ChessSquareFace(
                                            square: square,
                                            piece: position[square],
                                            row: row,
                                            column: column,
                                            side: squareSide,
                                            selected: activeSelection == square,
                                            target: targets.contains(square),
                                            lastMove: lastMove.contains(square),
                                            check: checkedKing == square,
                                            palette: palette
                                        ),
                                        legal: legal
                                    )
                                }
                            }
                        }
                    }
                }
                if let promotion, interactive {
                    ChessPromotionPicker(
                        request: promotion,
                        side: position.sideToMove,
                        squareSide: squareSide,
                        palette: palette,
                        onPick: { kind in
                            self.promotion = nil
                            onMove(promotion.move(kind))
                        },
                        onCancel: { self.promotion = nil }
                    )
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
        .accessibilityLabel("Chess board")
        .onChange(of: board.fen) { reset() }
        .onChange(of: interactive) { reset() }
        .onChange(of: perspective) { reset() }
    }

    private func squareButton(_ face: ChessSquareFace, legal: ChessLegalMoves) -> some View {
        Button {
            tap(face.square, legal: legal)
        } label: {
            face
        }
        .buttonStyle(ChessSquareButtonStyle())
        .frame(width: face.side, height: face.side)
        .accessibilityLabel(face.spokenLabel)
        .accessibilityIdentifier(AccessibilityID.Game.chessSquare(face.square.name))
        .accessibilityAddTraits(face.selected ? .isSelected : [])
    }

    private func tap(_ square: ChessSquare, legal: ChessLegalMoves) {
        guard interactive, promotion == nil else { return }
        switch ChessTapOutcome.resolve(tap: square, selected: selected, legal: legal) {
        case .select(let next):
            selected = next
        case .move(let move):
            selected = nil
            onMove(move)
        case .promote(let request):
            selected = nil
            promotion = request
        }
    }

    private func reset() {
        selected = nil
        promotion = nil
    }
}

private struct ChessSquareFace: View {
    let square: ChessSquare
    let piece: ChessPiece?
    let row: Int
    let column: Int
    let side: CGFloat
    let selected: Bool
    let target: Bool
    let lastMove: Bool
    let check: Bool
    let palette: BoardPalette

    var spokenLabel: String {
        let content = piece?.label ?? "empty"
        var parts = ["\(square.name), \(content)"]
        if check { parts.append("in check") }
        if target { parts.append("legal target") }
        return parts.joined(separator: ", ")
    }

    var body: some View {
        let base = square.isDark ? palette.chessDark : palette.chessLight
        let ink = square.isDark ? palette.chessLight : palette.chessDark
        ZStack {
            Rectangle().fill(base)
            if lastMove {
                Rectangle().fill(palette.chessLast)
            }
            if selected {
                Rectangle().fill(palette.chessMark)
            }
            if check {
                Rectangle().fill(
                    RadialGradient(
                        stops: [
                            .init(color: palette.chessCheck, location: 0),
                            .init(color: palette.chessCheck, location: 0.35),
                            .init(color: palette.chessCheck.opacity(0), location: 0.72),
                        ],
                        center: .center,
                        startRadius: 0,
                        endRadius: side * 0.71
                    )
                )
            }
            if let piece {
                ChessPieceView(piece: piece)
                    .frame(width: side * 0.86, height: side * 0.86)
            }
            if target {
                if piece == nil {
                    Circle()
                        .fill(palette.chessMark)
                        .frame(width: side * 0.28, height: side * 0.28)
                } else {
                    Circle()
                        .strokeBorder(palette.chessMark, lineWidth: side * 0.08)
                        .padding(side * 0.04)
                }
            }
            if column == 0 {
                coordinate(square.rankName, color: ink)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                    .padding(.leading, side * 0.06)
                    .padding(.top, side * 0.03)
            }
            if row == 7 {
                coordinate(square.fileName, color: ink)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomTrailing)
                    .padding(.trailing, side * 0.07)
                    .padding(.bottom, side * 0.02)
            }
        }
        .frame(width: side, height: side)
        .contentShape(Rectangle())
    }

    private func coordinate(_ text: String, color: Color) -> some View {
        Text(text)
            .font(.system(size: max(8, side * 0.2), weight: .semibold, design: .rounded))
            .foregroundStyle(color)
            .accessibilityHidden(true)
    }
}

private struct ChessSquareButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
    }
}

private struct ChessPromotionPicker: View {
    let request: ChessPromotionRequest
    let side: ChessSide
    let squareSide: CGFloat
    let palette: BoardPalette
    let onPick: (ChessPieceKind) -> Void
    let onCancel: () -> Void

    var body: some View {
        let button = max(44, squareSide * 1.05)
        ZStack {
            Rectangle()
                .fill(palette.scrim)
                .contentShape(Rectangle())
                .onTapGesture(perform: onCancel)
                .accessibilityHidden(true)
            HStack(spacing: 6) {
                ForEach(request.choices, id: \.self) { kind in
                    Button {
                        onPick(kind)
                    } label: {
                        ChessPieceView(piece: ChessPiece(kind: kind, side: side))
                            .padding(button * 0.1)
                            .frame(width: button, height: button)
                            .background(RoundedRectangle(cornerRadius: 9, style: .continuous).fill(.quaternary))
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Promote to \(kind.name)")
                    .accessibilityIdentifier("chess.promotion.\(kind.rawValue)")
                }
                Button(action: onCancel) {
                    Image(systemName: "xmark")
                        .font(.system(size: button * 0.32, weight: .semibold))
                        .foregroundStyle(.secondary)
                        .frame(width: button, height: button)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Cancel promotion")
                .accessibilityIdentifier("chess.promotion.cancel")
            }
            .padding(8)
            .background {
                RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .fill(.background)
                    .shadow(color: .black.opacity(0.25), radius: 18, y: 8)
            }
            .overlay {
                RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .strokeBorder(.separator, lineWidth: 1)
            }
            .accessibilityElement(children: .contain)
            .accessibilityLabel("Choose a piece")
            .accessibilityAddTraits(.isModal)
        }
    }
}
