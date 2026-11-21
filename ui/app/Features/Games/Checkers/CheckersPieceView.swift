import SwiftUI

struct CheckersPieceView: View {
    let piece: CheckersPiece

    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        let palette = BoardPalette.resolve(colorScheme)
        let fill = piece.side == .black ? palette.pieceBlack : palette.pieceWhite
        let detail = piece.side == .black ? palette.pieceWhite : palette.pieceBlack
        let edge = palette.pieceBlack
        let king = piece.isKing
        Canvas { context, size in
            let side = min(size.width, size.height)
            context.translateBy(x: (size.width - side) / 2, y: (size.height - side) / 2)
            context.scaleBy(x: side / 100, y: side / 100)
            let disc = Path(ellipseIn: CGRect(x: 6, y: 6, width: 88, height: 88))
            context.fill(disc, with: .color(fill))
            context.stroke(disc, with: .color(edge), lineWidth: 4)
            context.stroke(Path(ellipseIn: CGRect(x: 19, y: 19, width: 62, height: 62)), with: .color(detail.opacity(0.35)), lineWidth: 3)
            if king {
                let crown = Self.crown
                context.fill(crown, with: .color(detail))
                context.stroke(crown, with: .color(detail), style: StrokeStyle(lineWidth: 3, lineJoin: .round))
            }
        }
        .aspectRatio(1, contentMode: .fit)
        .shadow(color: .black.opacity(0.3), radius: 1, y: 1)
        .accessibilityHidden(true)
    }

    private static var crown: Path {
        Path { path in
            path.move(to: CGPoint(x: 30, y: 62))
            path.addLine(to: CGPoint(x: 70, y: 62))
            path.addLine(to: CGPoint(x: 74, y: 36))
            path.addLine(to: CGPoint(x: 62, y: 46))
            path.addLine(to: CGPoint(x: 50, y: 30))
            path.addLine(to: CGPoint(x: 38, y: 46))
            path.addLine(to: CGPoint(x: 26, y: 36))
            path.closeSubpath()
        }
    }
}
