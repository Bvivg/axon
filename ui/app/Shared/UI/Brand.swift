import SwiftUI

enum Brand {
    static let ink = Color(red: 15 / 255, green: 15 / 255, blue: 20 / 255)
    static let glow = Color(red: 124 / 255, green: 92 / 255, blue: 1)

    static var page: Color { Theme.background }

    static var card: Color { Theme.card }
}

struct AxonGlyph: View {
    var foreground: Color = .white

    var body: some View {
        Canvas { context, size in
            let scale = min(size.width, size.height) / 512
            context.translateBy(x: (size.width - 512 * scale) / 2, y: (size.height - 512 * scale) / 2)
            context.scaleBy(x: scale, y: scale)
            context.fill(Path(ellipseIn: CGRect(x: 54, y: 200, width: 112, height: 112)), with: .color(Brand.glow))
            for x in [184.0, 248.0] {
                context.fill(Path(roundedRect: CGRect(x: x, y: 238, width: 50, height: 36), cornerRadius: 18), with: .color(foreground))
            }
            var branches = Path()
            branches.move(to: CGPoint(x: 316, y: 256))
            branches.addCurve(to: CGPoint(x: 406, y: 162), control1: CGPoint(x: 358, y: 256), control2: CGPoint(x: 370, y: 162))
            branches.move(to: CGPoint(x: 316, y: 256))
            branches.addLine(to: CGPoint(x: 434, y: 256))
            branches.move(to: CGPoint(x: 316, y: 256))
            branches.addCurve(to: CGPoint(x: 406, y: 350), control1: CGPoint(x: 358, y: 256), control2: CGPoint(x: 370, y: 350))
            context.stroke(branches, with: .color(foreground), style: StrokeStyle(lineWidth: 22, lineCap: .round))
            for point in [CGPoint(x: 406, y: 162), CGPoint(x: 434, y: 256), CGPoint(x: 406, y: 350)] {
                context.fill(Path(ellipseIn: CGRect(x: point.x - 24, y: point.y - 24, width: 48, height: 48)), with: .color(foreground))
            }
        }
        .accessibilityHidden(true)
    }
}

struct AxonAppIcon: View {
    var size: CGFloat

    var body: some View {
        AxonGlyph()
            .frame(width: size, height: size)
            .background(Brand.ink, in: .rect(cornerRadius: size * 0.2237, style: .continuous))
            .shadow(color: Brand.glow.opacity(0.35), radius: size * 0.18, y: size * 0.06)
    }
}

struct GameBadge: View {
    let kind: GameKind
    var size: CGFloat = 36

    var body: some View {
        IconTile(size: size) {
            GameGlyph(kind: kind, size: size * 0.5)
        }
    }
}

struct GameGlyph: View {
    let kind: GameKind
    var size: CGFloat = 18

    var body: some View {
        switch kind {
        case .ticTacToe:
            HStack(spacing: size * 0.04) {
                Image(systemName: "xmark")
                Image(systemName: "circle")
            }
            .font(.system(size: size * 0.52, weight: .semibold))
        case .chess:
            Text(verbatim: "\u{265E}")
                .font(.system(size: size * 1.25))
        default:
            Image(systemName: kind.symbol)
                .font(.system(size: size * 0.86, weight: .regular))
        }
    }
}

struct Avatar: View {
    let name: String
    var url: URL?
    var size: CGFloat = 40
    var cornerRadius: CGFloat?

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: cornerRadius ?? size * 0.3, style: .continuous)
        ZStack {
            shape.fill(Theme.accent)
            Text(initials)
                .font(.geist(size * 0.36, .semibold))
                .foregroundStyle(Theme.primary)
            if let url {
                AsyncImage(url: url) { image in
                    image.resizable().scaledToFill()
                } placeholder: {
                    Color.clear
                }
            }
        }
        .frame(width: size, height: size)
        .clipShape(shape)
        .accessibilityHidden(true)
    }

    private var initials: String {
        let words = name.split(whereSeparator: \.isWhitespace).prefix(2)
        let letters = words.compactMap(\.first).map { String($0).uppercased() }.joined()
        return letters.isEmpty ? "?" : letters
    }
}

struct Monogram: View {
    let name: String
    var size: CGFloat = 40

    var body: some View {
        Avatar(name: name, size: size, cornerRadius: size / 2)
    }
}

extension User {
    var shownName: String {
        if hasDisplayName, !displayName.isEmpty { return displayName }
        if hasUsername, !username.isEmpty { return username }
        return email
    }
}
