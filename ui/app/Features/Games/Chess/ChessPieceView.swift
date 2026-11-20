import SwiftUI

struct ChessPieceView: View {
    let piece: ChessPiece

    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        let palette = BoardPalette.resolve(colorScheme)
        let fill = piece.side == .white ? palette.pieceWhite : palette.pieceBlack
        let detail = piece.side == .white ? palette.pieceBlack : palette.pieceWhite
        let edge = palette.pieceBlack
        Canvas { context, size in
            let side = min(size.width, size.height)
            context.translateBy(x: (size.width - side) / 2, y: (size.height - side) / 2)
            context.scaleBy(x: side / 100, y: side / 100)
            for element in ChessGlyph.elements(piece.kind) {
                switch element {
                case .body(let path):
                    context.fill(path, with: .color(fill))
                    context.stroke(path, with: .color(edge), style: StrokeStyle(lineWidth: 4, lineJoin: .round))
                case .detailLine(let path):
                    context.stroke(path, with: .color(detail), style: StrokeStyle(lineWidth: 4, lineCap: .round))
                case .detailDot(let path):
                    context.fill(path, with: .color(detail))
                }
            }
        }
        .aspectRatio(1, contentMode: .fit)
        .accessibilityHidden(true)
    }
}

enum ChessGlyph {
    enum Element {
        case body(Path)
        case detailLine(Path)
        case detailDot(Path)
    }

    static func elements(_ kind: ChessPieceKind) -> [Element] {
        switch kind {
        case .pawn:
            [
                .body(circle(50, 31, 12)),
                .body(Path { path in
                    path.move(to: CGPoint(x: 38, y: 79))
                    path.addLine(to: CGPoint(x: 42, y: 54))
                    path.addQuadCurve(to: CGPoint(x: 58, y: 54), control: CGPoint(x: 50, y: 47))
                    path.addLine(to: CGPoint(x: 62, y: 79))
                    path.closeSubpath()
                }),
                base,
            ]
        case .rook:
            [
                .body(polygon([(28, 19), (38, 19), (38, 27), (44, 27), (44, 19), (56, 19), (56, 27), (62, 27), (62, 19), (72, 19), (72, 40), (28, 40)])),
                .body(polygon([(34, 40), (66, 40), (68, 79), (32, 79)])),
                base,
            ]
        case .bishop:
            [
                .body(circle(50, 13, 5)),
                .body(Path { path in
                    path.move(to: CGPoint(x: 50, y: 19))
                    path.addCurve(to: CGPoint(x: 58, y: 62), control1: CGPoint(x: 66, y: 31), control2: CGPoint(x: 66, y: 52))
                    path.addLine(to: CGPoint(x: 62, y: 79))
                    path.addLine(to: CGPoint(x: 38, y: 79))
                    path.addLine(to: CGPoint(x: 42, y: 62))
                    path.addCurve(to: CGPoint(x: 50, y: 19), control1: CGPoint(x: 34, y: 52), control2: CGPoint(x: 34, y: 31))
                    path.closeSubpath()
                }),
                .detailLine(Path { path in
                    path.move(to: CGPoint(x: 55, y: 32))
                    path.addLine(to: CGPoint(x: 46, y: 45))
                }),
                base,
            ]
        case .knight:
            [
                .body(Path { path in
                    path.move(to: CGPoint(x: 36, y: 79))
                    path.addLine(to: CGPoint(x: 40, y: 58))
                    path.addCurve(to: CGPoint(x: 29, y: 40), control1: CGPoint(x: 30, y: 54), control2: CGPoint(x: 25, y: 46))
                    path.addLine(to: CGPoint(x: 46, y: 25))
                    path.addLine(to: CGPoint(x: 46, y: 15))
                    path.addLine(to: CGPoint(x: 54, y: 23))
                    path.addCurve(to: CGPoint(x: 72, y: 56), control1: CGPoint(x: 67, y: 25), control2: CGPoint(x: 75, y: 39))
                    path.addLine(to: CGPoint(x: 68, y: 79))
                    path.closeSubpath()
                }),
                .detailDot(circle(52, 36, 3.5)),
                base,
            ]
        case .queen:
            [
                .body(circle(24, 29, 5)),
                .body(circle(50, 20, 5)),
                .body(circle(76, 29, 5)),
                .body(polygon([(31, 79), (24, 35), (38, 53), (50, 26), (62, 53), (76, 35), (69, 79)])),
                base,
            ]
        case .king:
            [
                .body(polygon([(47, 11), (53, 11), (53, 19), (60, 19), (60, 25), (53, 25), (53, 34), (47, 34), (47, 25), (40, 25), (40, 19), (47, 19)])),
                .body(Path { path in
                    path.move(to: CGPoint(x: 32, y: 79))
                    path.addLine(to: CGPoint(x: 28, y: 47))
                    path.addQuadCurve(to: CGPoint(x: 72, y: 47), control: CGPoint(x: 50, y: 37))
                    path.addLine(to: CGPoint(x: 68, y: 79))
                    path.closeSubpath()
                }),
                base,
            ]
        }
    }

    private static let base = Element.body(Path(roundedRect: CGRect(x: 22, y: 79, width: 56, height: 11), cornerRadius: 4))

    private static func circle(_ x: CGFloat, _ y: CGFloat, _ radius: CGFloat) -> Path {
        Path(ellipseIn: CGRect(x: x - radius, y: y - radius, width: radius * 2, height: radius * 2))
    }

    private static func polygon(_ points: [(CGFloat, CGFloat)]) -> Path {
        Path { path in
            path.addLines(points.map { CGPoint(x: $0.0, y: $0.1) })
            path.closeSubpath()
        }
    }
}
