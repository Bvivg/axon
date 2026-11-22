import SwiftUI

struct QuoridorWallsLeftView: View {
    private let seat: Int
    private let count: Int

    @Environment(\.colorScheme) private var colorScheme

    init(seat: Int, count: Int) {
        self.seat = seat
        self.count = count
    }

    var body: some View {
        let palette = BoardPalette.resolve(colorScheme)
        let names = GameKind.quoridor.seatNames
        let name = names.indices.contains(seat) ? names[seat] : ""
        HStack(spacing: 8) {
            QuoridorPawn(color: palette.seat(seat))
                .frame(width: 12, height: 12)
            Text(name)
                .font(.axSubheadline.weight(.medium))
            HStack(spacing: 3) {
                ForEach(0..<Quoridor.wallsEach, id: \.self) { index in
                    Capsule()
                        .fill(index < count ? AnyShapeStyle(palette.quoridorWall) : AnyShapeStyle(.separator))
                        .frame(width: 3, height: 12)
                }
            }
            Text("\(count)")
                .font(.axMono(13, relativeTo: .footnote))
                .foregroundStyle(Theme.muted)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(name), \(count) \(count == 1 ? "wall" : "walls") left")
    }
}

struct QuoridorPawn: View {
    let color: Color

    var body: some View {
        Circle()
            .fill(color)
            .overlay {
                Circle().strokeBorder(.background, lineWidth: 1.5)
            }
            .shadow(color: .black.opacity(0.3), radius: 1.5, y: 1)
            .accessibilityHidden(true)
    }
}
