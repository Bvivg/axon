#if canImport(AppKit)
import AppKit
import SwiftUI
import Testing
@testable import Axon

@MainActor
final class MoveRecorder {
    var moves: [String] = []
}

@MainActor
struct BoardInteractionTests {
    private let side: CGFloat = 402

    private func host(_ view: some View) -> NSWindow {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: side, height: side),
            styleMask: [.borderless],
            backing: .buffered,
            defer: false
        )
        window.isReleasedWhenClosed = false
        window.contentView = NSHostingView(rootView: view.frame(width: side, height: side))
        window.orderFront(nil)
        window.contentView?.layoutSubtreeIfNeeded()
        settle()
        return window
    }

    private func settle() {
        RunLoop.main.run(until: Date().addingTimeInterval(0.05))
    }

    private func click(_ window: NSWindow, at point: CGPoint) throws {
        let location = NSPoint(x: point.x, y: side - point.y)
        for type in [NSEvent.EventType.leftMouseDown, .leftMouseUp] {
            let event = try #require(NSEvent.mouseEvent(
                with: type,
                location: location,
                modifierFlags: [],
                timestamp: ProcessInfo.processInfo.systemUptime,
                windowNumber: window.windowNumber,
                context: nil,
                eventNumber: 0,
                clickCount: 1,
                pressure: 1
            ))
            window.sendEvent(event)
        }
        settle()
    }

    private func center(_ rect: CGRect) -> CGPoint {
        CGPoint(x: rect.midX, y: rect.midY)
    }

    @Test func tapsEmptyTicTacToeCellsOnlyWhenInteractive() throws {
        let recorder = MoveRecorder()
        var board = TicTacToeBoardState()
        board.cells = [.x, .unspecified, .unspecified, .unspecified, .o, .unspecified, .unspecified, .unspecified, .unspecified]
        let cell = side / 3
        let live = host(TicTacToeBoardView(board: board, interactive: true, onTap: { recorder.moves.append(String($0)) }))
        try click(live, at: CGPoint(x: cell * 2.5, y: cell * 2.5))
        try click(live, at: CGPoint(x: cell * 0.5, y: cell * 0.5))
        live.close()
        let idle = host(TicTacToeBoardView(board: board, interactive: false, onTap: { recorder.moves.append(String($0)) }))
        try click(idle, at: CGPoint(x: cell * 1.5, y: cell * 0.5))
        idle.close()
        #expect(recorder.moves == ["8"])
    }

    @Test func selectsThenMovesChessPieces() throws {
        let recorder = MoveRecorder()
        var board = ChessBoardState()
        board.fen = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"
        board.legalMoves = ["e2e3", "e2e4", "g1f3", "g1h3"]
        let square = side / 8
        func point(_ name: String, perspective: Int) throws -> CGPoint {
            let target = try #require(ChessSquare(name))
            let column = perspective == 1 ? 7 - target.file : target.file
            let row = perspective == 1 ? target.rank : 7 - target.rank
            return CGPoint(x: (CGFloat(column) + 0.5) * square, y: (CGFloat(row) + 0.5) * square)
        }
        let window = host(ChessBoardView(board: board, perspective: 0, interactive: true, onMove: { recorder.moves.append($0) }))
        try click(window, at: try point("e4", perspective: 0))
        try click(window, at: try point("e2", perspective: 0))
        try click(window, at: try point("e4", perspective: 0))
        try click(window, at: try point("g1", perspective: 0))
        try click(window, at: try point("g1", perspective: 0))
        try click(window, at: try point("f3", perspective: 0))
        window.close()
        #expect(recorder.moves == ["e2e4"])

        board.fen = "1r4k1/P4ppp/8/8/8/8/5PPP/6K1 w - - 0 32"
        board.legalMoves = ["a7a8q", "a7a8r", "a7a8b", "a7a8n", "a7b8q", "a7b8r", "a7b8b", "a7b8n"]
        let flipped = host(ChessBoardView(board: board, perspective: 1, interactive: true, onMove: { recorder.moves.append($0) }))
        try click(flipped, at: try point("a7", perspective: 1))
        try click(flipped, at: try point("b8", perspective: 1))
        let pickerButton = max(44, square * 1.05)
        let rowWidth = pickerButton * 5 + 6 * 4
        let knight = CGPoint(x: side / 2 - rowWidth / 2 + pickerButton * 3.5 + 6 * 3, y: side / 2)
        try click(flipped, at: knight)
        flipped.close()
        #expect(recorder.moves == ["e2e4", "a7b8n"])
    }

    @Test func placesQuoridorWallsWithTwoTapsAndMovesPawns() throws {
        let recorder = MoveRecorder()
        let board = BoardSnapshotTests.midGameQuoridor()
        let window = host(QuoridorBoardView(board: board, perspective: 0, interactive: true, onMove: { recorder.moves.append($0) }))
        let bars = 2 * (QuoridorWallsLeftView(seat: 0, count: 7).hostedHeight + 8)
        let boardSide = side - bars
        let layout = QuoridorBoardLayout(side: boardSide)
        let origin = CGPoint(x: (side - boardSide) / 2, y: bars / 2)
        let view = QuoridorPerspective(perspective: 0)
        func shifted(_ point: CGPoint) -> CGPoint {
            CGPoint(x: point.x + origin.x, y: point.y + origin.y)
        }
        let wall = try #require(WallSlot.all(perspective: view).first { $0.wall.notation == "c2h" && $0.isPrimary })
        let wallPoint = shifted(center(layout.slotRect(orientation: wall.orientation, groove: wall.groove, segment: wall.segment)))
        let near = CGPoint(x: wallPoint.x, y: wallPoint.y - layout.slotReach + 2)
        try click(window, at: wallPoint)
        #expect(recorder.moves.isEmpty)
        try click(window, at: near)
        #expect(recorder.moves == ["c2h"])

        let target = try #require(QuoridorCell(notation: "d4"))
        let display = view.display(target)
        try click(window, at: shifted(center(layout.cellRect(displayRow: display.row, displayCol: display.col))))
        let blocked = try #require(QuoridorCell(notation: "e5"))
        let blockedDisplay = view.display(blocked)
        try click(window, at: shifted(center(layout.cellRect(displayRow: blockedDisplay.row, displayCol: blockedDisplay.col))))
        window.close()
        #expect(recorder.moves == ["c2h", "d4"])
    }
}

@MainActor
private extension View {
    var hostedHeight: CGFloat {
        NSHostingView(rootView: self).fittingSize.height
    }
}
#endif
