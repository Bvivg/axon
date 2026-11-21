import SwiftUI

struct TicTacToeBoardView: View {
    private let board: TicTacToeBoardState
    private let interactive: Bool
    private let onTap: (Int) -> Void

    @Environment(\.colorScheme) private var colorScheme

    init(board: TicTacToeBoardState, interactive: Bool, onTap: @escaping (Int) -> Void) {
        self.board = board
        self.interactive = interactive
        self.onTap = onTap
    }

    var body: some View {
        GeometryReader { proxy in
            let side = min(proxy.size.width, proxy.size.height)
            let gap = (side * 0.025).rounded()
            let cellSide = (side - gap * 2) / 3
            let palette = BoardPalette.resolve(colorScheme)
            let cells = TicTacToeCell.cells(of: board)
            ZStack(alignment: .topLeading) {
                VStack(spacing: gap) {
                    ForEach(0..<3, id: \.self) { row in
                        HStack(spacing: gap) {
                            ForEach(0..<3, id: \.self) { column in
                                cellButton(cells[row * 3 + column], side: cellSide, palette: palette)
                            }
                        }
                    }
                }
                if let line = TicTacToeCell.winningLine(of: board) {
                    WinningLine(from: center(of: line.from, cell: cellSide, gap: gap), to: center(of: line.to, cell: cellSide, gap: gap))
                        .stroke(palette.green, style: StrokeStyle(lineWidth: max(3, side * 0.012), lineCap: .round))
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                }
            }
            .frame(width: side, height: side)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .aspectRatio(1, contentMode: .fit)
    }

    private func cellButton(_ cell: TicTacToeCell, side: CGFloat, palette: BoardPalette) -> some View {
        let available = interactive && cell.isEmpty
        return Button {
            if available { onTap(cell.index) }
        } label: {
            TicTacToeCellFace(cell: cell, side: side, palette: palette)
        }
        .buttonStyle(TicTacToeCellButtonStyle(hoverable: available, palette: palette))
        .frame(width: side, height: side)
        .disabled(!available)
        .accessibilityLabel(cell.label)
        .accessibilityIdentifier(AccessibilityID.Game.ticTacToeCell(cell.index))
    }

    private func center(of index: Int, cell: CGFloat, gap: CGFloat) -> CGPoint {
        CGPoint(
            x: CGFloat(index % 3) * (cell + gap) + cell / 2,
            y: CGFloat(index / 3) * (cell + gap) + cell / 2
        )
    }
}

private struct TicTacToeCellFace: View {
    let cell: TicTacToeCell
    let side: CGFloat
    let palette: BoardPalette

    var body: some View {
        let radius = side * 0.12
        ZStack {
            RoundedRectangle(cornerRadius: radius, style: .continuous)
                .fill(cell.winning ? AnyShapeStyle(palette.green.opacity(0.12)) : AnyShapeStyle(.background.secondary))
            RoundedRectangle(cornerRadius: radius, style: .continuous)
                .strokeBorder(cell.winning ? AnyShapeStyle(palette.green.opacity(0.6)) : AnyShapeStyle(.separator), lineWidth: 1)
            TicTacToeMarkShape(mark: cell.mark)
                .stroke(markStyle, style: StrokeStyle(lineWidth: side * 0.075, lineCap: .round))
                .frame(width: side * 0.42, height: side * 0.42)
        }
        .frame(width: side, height: side)
        .contentShape(RoundedRectangle(cornerRadius: radius, style: .continuous))
    }

    private var markStyle: AnyShapeStyle {
        if cell.winning { return AnyShapeStyle(palette.green) }
        return cell.mark == .x ? AnyShapeStyle(palette.violet) : AnyShapeStyle(.primary)
    }
}

private struct TicTacToeCellButtonStyle: ButtonStyle {
    let hoverable: Bool
    let palette: BoardPalette

    func makeBody(configuration: Configuration) -> some View {
        TicTacToeCellPressable(configuration: configuration, hoverable: hoverable, palette: palette)
    }
}

private struct TicTacToeCellPressable: View {
    let configuration: ButtonStyle.Configuration
    let hoverable: Bool
    let palette: BoardPalette
    @State private var hovering = false

    var body: some View {
        configuration.label
            .overlay {
                if hoverable && (hovering || configuration.isPressed) {
                    GeometryReader { proxy in
                        RoundedRectangle(cornerRadius: proxy.size.width * 0.12, style: .continuous)
                            .fill(palette.violet.opacity(0.1))
                    }
                    .allowsHitTesting(false)
                }
            }
            .scaleEffect(configuration.isPressed && hoverable ? 0.97 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
            .onHover { hovering = $0 }
    }
}

struct TicTacToeMarkShape: Shape {
    let mark: Mark

    func path(in rect: CGRect) -> Path {
        var path = Path()
        switch mark {
        case .x:
            path.move(to: CGPoint(x: rect.minX, y: rect.minY))
            path.addLine(to: CGPoint(x: rect.maxX, y: rect.maxY))
            path.move(to: CGPoint(x: rect.maxX, y: rect.minY))
            path.addLine(to: CGPoint(x: rect.minX, y: rect.maxY))
        case .o:
            path.addEllipse(in: rect.insetBy(dx: rect.width * 0.02, dy: rect.height * 0.02))
        default:
            break
        }
        return path
    }
}

private struct WinningLine: Shape {
    let from: CGPoint
    let to: CGPoint

    func path(in rect: CGRect) -> Path {
        var path = Path()
        path.move(to: from)
        path.addLine(to: to)
        return path
    }
}
