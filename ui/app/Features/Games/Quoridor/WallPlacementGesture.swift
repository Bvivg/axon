import SwiftUI

struct WallSlot: Identifiable, Sendable, Hashable {
    let orientation: QuoridorWallOrientation
    let groove: Int
    let segment: Int
    let wall: QuoridorWall

    var id: String { "\(orientation.rawValue)\(groove)-\(segment)" }
    var isPrimary: Bool { segment < Quoridor.anchors }

    static func all(perspective: QuoridorPerspective) -> [WallSlot] {
        QuoridorWallOrientation.allCases.flatMap { orientation in
            (0..<Quoridor.anchors).flatMap { groove in
                (0..<Quoridor.size).compactMap { segment -> WallSlot? in
                    let display = QuoridorDisplayWall(orientation: orientation, groove: groove, segment: min(segment, Quoridor.anchors - 1))
                    guard let wall = perspective.wall(display) else { return nil }
                    return WallSlot(orientation: orientation, groove: groove, segment: segment, wall: wall)
                }
            }
        }
    }
}

struct WallPlacementState: Sendable, Equatable {
    private(set) var hovered: WallSlot?
    private(set) var pending: QuoridorWall?

    init(pending: QuoridorWall? = nil) {
        self.pending = pending
    }

    var preview: QuoridorWall? { hovered?.wall ?? pending }

    mutating func pointerEntered(_ slot: WallSlot) {
        hovered = slot
    }

    mutating func pointerExited(_ slot: WallSlot) {
        if hovered == slot { hovered = nil }
    }

    mutating func tap(_ slot: WallSlot) -> QuoridorWall? {
        if hovered == slot || pending == slot.wall {
            pending = nil
            return slot.wall
        }
        pending = slot.wall
        return nil
    }

    mutating func cancelPending() {
        pending = nil
    }

    mutating func retain(legal: Set<QuoridorWall>) {
        if let hovered, !legal.contains(hovered.wall) { self.hovered = nil }
        if let pending, !legal.contains(pending) { self.pending = nil }
    }
}

struct WallPlacementGesture: ViewModifier {
    let slot: WallSlot
    @Binding var state: WallPlacementState
    let onPlace: (QuoridorWall) -> Void

    func body(content: Content) -> some View {
        Button {
            if let wall = state.tap(slot) {
                onPlace(wall)
            }
        } label: {
            content.contentShape(Rectangle())
        }
        .buttonStyle(WallSlotButtonStyle())
        .onHover { inside in
            if inside {
                state.pointerEntered(slot)
            } else {
                state.pointerExited(slot)
            }
        }
        .modifier(WallSlotAccessibility(slot: slot, onPlace: onPlace))
    }
}

private struct WallSlotButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
    }
}

private struct WallSlotAccessibility: ViewModifier {
    let slot: WallSlot
    let onPlace: (QuoridorWall) -> Void

    func body(content: Content) -> some View {
        if slot.isPrimary {
            content
                .accessibilityLabel("Wall \(slot.wall.notation)")
                .accessibilityIdentifier(AccessibilityID.Game.quoridorWall(slot.wall.notation))
                .accessibilityAction { onPlace(slot.wall) }
        } else {
            content.accessibilityHidden(true)
        }
    }
}
