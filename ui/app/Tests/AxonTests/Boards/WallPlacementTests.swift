import CoreGraphics
import Testing
@testable import Axon

struct WallPlacementTests {
    @Test(arguments: [0, 1])
    func everyWallHasExactlyOnePrimarySlot(perspective: Int) {
        let view = QuoridorPerspective(perspective: perspective)
        let slots = WallSlot.all(perspective: view)
        #expect(slots.count == 144)
        #expect(Set(slots.map(\.id)).count == 144)
        let primary = slots.filter(\.isPrimary)
        #expect(primary.count == 128)
        #expect(Set(primary.map(\.wall)) == Set(QuoridorNotationTests.allWalls))
        for slot in primary {
            let display = view.display(slot.wall)
            #expect(display.orientation == slot.orientation)
            #expect(display.groove == slot.groove)
            #expect(display.segment == slot.segment)
        }
    }

    @Test(arguments: [0, 1])
    func edgeSlotsReuseTheLastAnchor(perspective: Int) {
        let view = QuoridorPerspective(perspective: perspective)
        let slots = WallSlot.all(perspective: view)
        for edge in slots where !edge.isPrimary {
            #expect(edge.segment == 8)
            let twin = slots.first { $0.orientation == edge.orientation && $0.groove == edge.groove && $0.segment == 7 }
            #expect(twin?.wall == edge.wall)
        }
    }

    @Test(arguments: [0, 1])
    func slotsSitOnTheirWalls(perspective: Int) {
        let view = QuoridorPerspective(perspective: perspective)
        let layout = QuoridorBoardLayout(side: 402)
        for slot in WallSlot.all(perspective: view) {
            let slotRect = layout.slotRect(orientation: slot.orientation, groove: slot.groove, segment: slot.segment)
            let wallRect = layout.wallRect(view.display(slot.wall))
            #expect(wallRect.contains(CGPoint(x: slotRect.midX, y: slotRect.midY)))
            #expect(slotRect.height >= 20 || slotRect.width >= 20)
        }
    }

    @Test func slotsOfOneGrooveDoNotOverlap() {
        let layout = QuoridorBoardLayout(side: 402)
        let slots = WallSlot.all(perspective: QuoridorPerspective(perspective: 0))
        for orientation in QuoridorWallOrientation.allCases {
            let rects = slots.filter { $0.orientation == orientation && $0.groove == 3 }
                .map { layout.slotRect(orientation: $0.orientation, groove: $0.groove, segment: $0.segment) }
            for (index, rect) in rects.enumerated() {
                for other in rects[(index + 1)...] {
                    #expect(!rect.insetBy(dx: 0.01, dy: 0.01).intersects(other))
                }
            }
        }
    }

    @Test func touchPreviewsThenConfirms() throws {
        let slots = WallSlot.all(perspective: QuoridorPerspective(perspective: 0))
        let slot = try #require(slots.first { $0.wall.notation == "e3h" && $0.isPrimary })
        var state = WallPlacementState()
        #expect(state.tap(slot) == nil)
        #expect(state.preview == slot.wall)
        #expect(state.tap(slot) == slot.wall)
        #expect(state.preview == nil)
    }

    @Test func touchOnAnotherWallMovesThePreview() throws {
        let slots = WallSlot.all(perspective: QuoridorPerspective(perspective: 1))
        let first = try #require(slots.first { $0.wall.notation == "c4v" && $0.isPrimary })
        let second = try #require(slots.first { $0.wall.notation == "d4v" && $0.isPrimary })
        var state = WallPlacementState()
        #expect(state.tap(first) == nil)
        #expect(state.tap(second) == nil)
        #expect(state.preview == second.wall)
        state.cancelPending()
        #expect(state.preview == nil)
    }

    @Test func pointerPlacesOnFirstClick() throws {
        let slots = WallSlot.all(perspective: QuoridorPerspective(perspective: 0))
        let slot = try #require(slots.first { $0.wall.notation == "h1v" && !$0.isPrimary })
        var state = WallPlacementState()
        state.pointerEntered(slot)
        #expect(state.preview == slot.wall)
        #expect(state.tap(slot) == slot.wall)
    }

    @Test func pointerExitOfPreviousSlotKeepsTheNewHover() throws {
        let slots = WallSlot.all(perspective: QuoridorPerspective(perspective: 0))
        let first = try #require(slots.first { $0.orientation == .horizontal && $0.groove == 2 && $0.segment == 7 })
        let second = try #require(slots.first { $0.orientation == .horizontal && $0.groove == 2 && $0.segment == 8 })
        var state = WallPlacementState()
        state.pointerEntered(first)
        state.pointerEntered(second)
        state.pointerExited(first)
        #expect(state.preview == second.wall)
        #expect(state.tap(second) == second.wall)
        state.pointerExited(second)
        #expect(state.preview == nil)
    }

    @Test func retainDropsWallsThatAreNoLongerLegal() throws {
        let slots = WallSlot.all(perspective: QuoridorPerspective(perspective: 0))
        let hovered = try #require(slots.first { $0.wall.notation == "a1h" && $0.isPrimary })
        let pending = try #require(QuoridorWall(notation: "b2v"))
        var state = WallPlacementState(pending: pending)
        state.pointerEntered(hovered)
        state.retain(legal: [pending])
        #expect(state.preview == pending)
        state.retain(legal: [])
        #expect(state.preview == nil)
    }
}
