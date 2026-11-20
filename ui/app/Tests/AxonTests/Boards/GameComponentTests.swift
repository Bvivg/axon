import Foundation
import Testing
@testable import Axon

struct MoveListTests {
    @Test func pairsChessMoves() {
        let rows = MoveListRow.rows(["e4", "e5", "Nf3", "Nc6", "Bb5"], layout: .pairs)
        #expect(rows.map(\.number) == [1, 2, 3])
        #expect(rows.map { $0.entries.map(\.text) } == [["e4", "e5"], ["Nf3", "Nc6"], ["Bb5"]])
        #expect(rows.map { $0.entries.map(\.ply) } == [[1, 2], [3, 4], [5]])
    }

    @Test func listsPlainMoves() {
        let rows = MoveListRow.rows(["e2", "e8", "e3h"], layout: .plain)
        #expect(rows.map(\.number) == [1, 2, 3])
        #expect(rows.map(\.id) == [0, 1, 2])
        #expect(rows.map { $0.entries.map(\.text) } == [["e2"], ["e8"], ["e3h"]])
    }

    @Test(arguments: [
        (MoveListLayout.pairs, 5, Int?.none, Int?.some(2)),
        (.pairs, 5, 4, 1),
        (.pairs, 5, 1, 0),
        (.pairs, 5, 0, 0),
        (.pairs, 0, nil, nil),
        (.plain, 3, nil, 2),
        (.plain, 3, 2, 1),
        (.plain, 3, 9, 2),
    ])
    func scrollsToTheHighlightedRow(layout: MoveListLayout, count: Int, highlighted: Int?, expected: Int?) {
        #expect(MoveListRow.scrollTarget(moveCount: count, highlightedPly: highlighted, layout: layout) == expected)
    }

    @Test func picksLayoutByGame() {
        #expect(MoveListLayout(.chess) == .pairs)
        #expect(MoveListLayout(.quoridor) == .plain)
    }
}

struct ClockReadoutTests {
    private let sampled = Date(timeIntervalSince1970: 1_000_000)

    @Test func countsDownWhileRunning() {
        let now = sampled.addingTimeInterval(1.5)
        #expect(ClockReadout.remaining(.seconds(60), running: true, sampledAt: sampled, at: now) == .milliseconds(58_500))
        #expect(ClockReadout.remaining(.seconds(60), running: false, sampledAt: sampled, at: now) == .seconds(60))
    }

    @Test func neverGoesBelowZero() {
        let now = sampled.addingTimeInterval(30)
        #expect(ClockReadout.remaining(.seconds(5), running: true, sampledAt: sampled, at: now) == .zero)
        #expect(ClockReadout.remaining(.seconds(-3), running: false, sampledAt: sampled, at: now) == .zero)
        #expect(ClockReadout.remaining(.seconds(5), running: true, sampledAt: sampled, at: sampled.addingTimeInterval(-2)) == .seconds(5))
    }

    @Test(arguments: [
        (Duration.seconds(600), "10:00"),
        (.milliseconds(599_001), "10:00"),
        (.seconds(599), "9:59"),
        (.milliseconds(9_200), "0:10"),
        (.seconds(65), "1:05"),
        (.milliseconds(1), "0:01"),
        (.zero, "0:00"),
    ])
    func formatsWithCeilingSeconds(duration: Duration, text: String) {
        #expect(ClockReadout.text(duration) == text)
    }

    @Test func flagsLowTime() {
        #expect(ClockReadout.isLow(.seconds(9), running: true))
        #expect(!ClockReadout.isLow(.seconds(9), running: false))
        #expect(ClockReadout.isLow(.zero, running: false))
        #expect(!ClockReadout.isLow(.seconds(11), running: true))
    }

    @Test func ticksOnSecondBoundaries() {
        let first = ClockReadout.firstTick(remaining: .milliseconds(59_300), sampledAt: sampled)
        let next = first.addingTimeInterval(1)
        let before = ClockReadout.remaining(.milliseconds(59_300), running: true, sampledAt: sampled, at: next.addingTimeInterval(-0.002))
        let after = ClockReadout.remaining(.milliseconds(59_300), running: true, sampledAt: sampled, at: next)
        #expect(first <= sampled)
        #expect(ClockReadout.text(before) == "1:00")
        #expect(ClockReadout.text(after) == "0:59")
    }
}
