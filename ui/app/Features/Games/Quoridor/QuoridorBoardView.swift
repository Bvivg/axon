import SwiftUI

struct QuoridorBoardView: View {
    private let board: QuoridorBoardState
    private let perspective: Int
    private let interactive: Bool
    private let onMove: (String) -> Void

    @State private var placement = WallPlacementState()
    @Environment(\.colorScheme) private var colorScheme

    init(board: QuoridorBoardState, perspective: Int, interactive: Bool, onMove: @escaping (String) -> Void) {
        self.init(board: board, perspective: perspective, interactive: interactive, placement: WallPlacementState(), onMove: onMove)
    }

    init(
        board: QuoridorBoardState,
        perspective: Int,
        interactive: Bool,
        placement: WallPlacementState,
        onMove: @escaping (String) -> Void
    ) {
        self.board = board
        self.perspective = perspective
        self.interactive = interactive
        self.onMove = onMove
        _placement = State(initialValue: placement)
    }

    var body: some View {
        let model = QuoridorBoardModel(board, interactive: interactive)
        let bottomSeat = perspective == 1 ? 1 : 0
        QuoridorBoardStack(spacing: 8) {
            QuoridorWallsLeftView(seat: 1 - bottomSeat, count: model.wallsLeft(seat: 1 - bottomSeat))
                .frame(maxWidth: .infinity, alignment: .leading)
            GeometryReader { proxy in
                grid(model: model, layout: QuoridorBoardLayout(side: min(proxy.size.width, proxy.size.height)))
            }
            QuoridorWallsLeftView(seat: bottomSeat, count: model.wallsLeft(seat: bottomSeat))
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .onChange(of: model.legalWalls) { placement.retain(legal: model.legalWalls) }
    }

    private func grid(model: QuoridorBoardModel, layout: QuoridorBoardLayout) -> some View {
        let palette = BoardPalette.resolve(colorScheme)
        let orientation = QuoridorPerspective(perspective: perspective)
        let preview = placement.preview.flatMap { model.legalWalls.contains($0) ? $0 : nil }
        return ZStack(alignment: .topLeading) {
            RoundedRectangle(cornerRadius: layout.side * 0.03, style: .continuous)
                .fill(palette.quoridorGroove)
            RoundedRectangle(cornerRadius: layout.side * 0.03, style: .continuous)
                .strokeBorder(.separator, lineWidth: 1)
            ForEach(0..<Quoridor.size, id: \.self) { row in
                ForEach(0..<Quoridor.size, id: \.self) { col in
                    if let cell = orientation.cell(displayRow: row, displayCol: col) {
                        cellButton(cell, row: row, col: col, model: model, layout: layout, palette: palette)
                    }
                }
            }
            ForEach(model.walls, id: \.self) { wall in
                wallShape(wall, layout: layout, orientation: orientation)
                    .fill(wall == model.lastWall ? palette.quoridorWallLast : palette.quoridorWall)
                    .allowsHitTesting(false)
            }
            if let preview {
                wallShape(preview, layout: layout, orientation: orientation)
                    .fill(palette.quoridorWall.opacity(0.55))
                    .allowsHitTesting(false)
            }
            if interactive {
                ForEach(WallSlot.all(perspective: orientation).filter { model.legalWalls.contains($0.wall) }) { slot in
                    let rect = layout.slotRect(orientation: slot.orientation, groove: slot.groove, segment: slot.segment)
                    Color.clear
                        .frame(width: rect.width, height: rect.height)
                        .modifier(WallPlacementGesture(slot: slot, state: $placement, onPlace: place))
                        .position(x: rect.midX, y: rect.midY)
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Quoridor board")
    }

    private func cellButton(_ cell: QuoridorCell, row: Int, col: Int, model: QuoridorBoardModel, layout: QuoridorBoardLayout, palette: BoardPalette) -> some View {
        let rect = layout.cellRect(displayRow: row, displayCol: col)
        let face = QuoridorCellFace(
            cell: cell,
            pawnSeat: model.seat(at: cell),
            target: model.targets.contains(cell),
            lastMove: model.lastCell == cell,
            rankLabel: col == 0,
            fileLabel: row == Quoridor.size - 1,
            side: layout.cell,
            palette: palette
        )
        return Button {
            placement.cancelPending()
            if face.target { onMove(cell.notation) }
        } label: {
            face
        }
        .buttonStyle(QuoridorCellButtonStyle())
        .frame(width: rect.width, height: rect.height)
        .accessibilityLabel(face.spokenLabel)
        .accessibilityIdentifier(AccessibilityID.Game.quoridorCell(cell.notation))
        .position(x: rect.midX, y: rect.midY)
    }

    private func wallShape(_ wall: QuoridorWall, layout: QuoridorBoardLayout, orientation: QuoridorPerspective) -> some Shape {
        let rect = layout.wallRect(orientation.display(wall))
        return Capsule().path(in: rect)
    }

    private func place(_ wall: QuoridorWall) {
        onMove(wall.notation)
    }
}

private struct QuoridorCellFace: View {
    let cell: QuoridorCell
    let pawnSeat: Int?
    let target: Bool
    let lastMove: Bool
    let rankLabel: Bool
    let fileLabel: Bool
    let side: CGFloat
    let palette: BoardPalette

    var spokenLabel: String {
        let names = GameKind.quoridor.seatNames
        var parts = [cell.notation]
        if let pawnSeat, names.indices.contains(pawnSeat) {
            parts.append("\(names[pawnSeat]) pawn")
        } else if target {
            parts.append("move here")
        } else {
            parts.append("empty")
        }
        if lastMove { parts.append("last move") }
        return parts.joined(separator: ", ")
    }

    var body: some View {
        let radius = side * 0.18
        ZStack {
            RoundedRectangle(cornerRadius: radius, style: .continuous)
                .fill(palette.quoridorCell)
            if lastMove {
                RoundedRectangle(cornerRadius: radius, style: .continuous)
                    .strokeBorder(palette.quoridorWallLast.opacity(0.6), lineWidth: max(1.5, side * 0.05))
            }
            if let pawnSeat {
                QuoridorPawn(color: palette.seat(pawnSeat))
                    .frame(width: side * 0.64, height: side * 0.64)
            }
            if target {
                Circle()
                    .fill(palette.quoridorTarget)
                    .frame(width: side * 0.3, height: side * 0.3)
            }
            if rankLabel {
                label(cell.rankName)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                    .padding(.leading, side * 0.1)
                    .padding(.top, side * 0.04)
            }
            if fileLabel {
                label(cell.fileName)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomTrailing)
                    .padding(.trailing, side * 0.1)
                    .padding(.bottom, side * 0.02)
            }
        }
        .frame(width: side, height: side)
        .contentShape(RoundedRectangle(cornerRadius: radius, style: .continuous))
    }

    private func label(_ text: String) -> some View {
        Text(text)
            .font(.system(size: max(7, side * 0.22), weight: .semibold, design: .rounded))
            .foregroundStyle(.secondary)
            .opacity(0.8)
            .accessibilityHidden(true)
    }
}

private struct QuoridorCellButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.85 : 1)
    }
}

struct QuoridorBoardStack: Layout {
    var spacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let metrics = metrics(for: proposal, subviews: subviews)
        return CGSize(width: Self.finite(proposal.width) ?? metrics.side, height: metrics.height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let metrics = metrics(for: ProposedViewSize(bounds.size), subviews: subviews)
        let x = bounds.minX + (bounds.width - metrics.side) / 2
        var y = bounds.minY + max(0, (bounds.height - metrics.height) / 2)
        for (index, subview) in subviews.enumerated() {
            let height = index == 1 ? metrics.side : metrics.bars[index]
            subview.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(width: metrics.side, height: height))
            y += height + spacing
        }
    }

    private func metrics(for proposal: ProposedViewSize, subviews: Subviews) -> (side: CGFloat, bars: [CGFloat], height: CGFloat) {
        let width = Self.finite(proposal.width)
        let bars = subviews.enumerated().map { index, subview in
            index == 1 ? 0 : subview.sizeThatFits(ProposedViewSize(width: width ?? Self.fallbackSide, height: nil)).height
        }
        let chrome = bars.reduce(0, +) + spacing * CGFloat(max(subviews.count - 1, 0))
        let height = Self.finite(proposal.height).map { $0 - chrome }
        let side: CGFloat = switch (width, height) {
        case let (width?, height?): min(width, height)
        case let (width?, nil): width
        case let (nil, height?): height
        case (nil, nil): Self.fallbackSide
        }
        return (max(0, side), bars, max(0, side) + chrome)
    }

    private static let fallbackSide: CGFloat = 360

    private static func finite(_ value: CGFloat?) -> CGFloat? {
        value.flatMap { $0.isFinite ? $0 : nil }
    }
}
