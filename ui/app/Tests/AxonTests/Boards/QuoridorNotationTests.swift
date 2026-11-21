import CoreGraphics
import Testing
@testable import Axon

struct QuoridorNotationTests {
    static let allWalls: [QuoridorWall] = QuoridorWallOrientation.allCases.flatMap { orientation in
        (0..<8).flatMap { row in
            (0..<8).compactMap { col in QuoridorWall(row: row, col: col, orientation: orientation) }
        }
    }

    @Test(arguments: [("a1", 0, 0), ("e5", 4, 4), ("e1", 0, 4), ("e9", 8, 4), ("i9", 8, 8)])
    func parsesCells(notation: String, row: Int, col: Int) throws {
        let cell = try #require(QuoridorCell(notation: notation))
        #expect(cell.row == row)
        #expect(cell.col == col)
        #expect(cell.notation == notation)
    }

    @Test(arguments: ["", "e", "j1", "a0", "e10", "E5", "e5h"])
    func rejectsBadCells(notation: String) {
        #expect(QuoridorCell(notation: notation) == nil)
    }

    @Test(arguments: [
        ("a1h", 0, 0, QuoridorWallOrientation.horizontal),
        ("e3h", 2, 4, .horizontal),
        ("e3v", 2, 4, .vertical),
        ("h8v", 7, 7, .vertical),
    ])
    func parsesWalls(notation: String, row: Int, col: Int, orientation: QuoridorWallOrientation) throws {
        let wall = try #require(QuoridorWall(notation: notation))
        #expect(wall.row == row)
        #expect(wall.col == col)
        #expect(wall.orientation == orientation)
        #expect(wall.notation == notation)
    }

    @Test(arguments: ["", "e5", "i1h", "a9h", "e5x", "e5hh", "e9v", "i9v"])
    func rejectsBadWalls(notation: String) {
        #expect(QuoridorWall(notation: notation) == nil)
    }

    @Test func roundTripsEveryWall() {
        #expect(Self.allWalls.count == 128)
        for wall in Self.allWalls {
            #expect(QuoridorWall(notation: wall.notation) == wall)
            #expect(QuoridorMove(notation: wall.notation) == .wall(wall))
        }
    }

    @Test func parsesMoves() throws {
        #expect(QuoridorMove(notation: "e5") == .pawn(try #require(QuoridorCell(notation: "e5"))))
        #expect(QuoridorMove(notation: "c4v") == .wall(try #require(QuoridorWall(notation: "c4v"))))
        #expect(QuoridorMove(notation: "zz") == nil)
        #expect(QuoridorMove(notation: "e5v")?.notation == "e5v")
    }

    @Test(arguments: [
        (0, "e1", 8, 4), (0, "e9", 0, 4), (0, "a1", 8, 0), (0, "i9", 0, 8),
        (1, "e1", 0, 4), (1, "e9", 8, 4), (1, "a1", 0, 8), (1, "i9", 8, 0),
    ])
    func mapsCellsToDisplay(perspective: Int, notation: String, row: Int, col: Int) throws {
        let orientation = QuoridorPerspective(perspective: perspective)
        let cell = try #require(QuoridorCell(notation: notation))
        let display = orientation.display(cell)
        #expect(display.row == row)
        #expect(display.col == col)
        #expect(orientation.cell(displayRow: row, displayCol: col) == cell)
    }

    @Test(arguments: [
        (0, "a1h", QuoridorWallOrientation.horizontal, 7, 0),
        (1, "a1h", .horizontal, 0, 7),
        (0, "a1v", .vertical, 0, 7),
        (1, "a1v", .vertical, 7, 0),
        (0, "e4h", .horizontal, 4, 4),
        (1, "e4h", .horizontal, 3, 3),
        (0, "h8v", .vertical, 7, 0),
    ])
    func mapsWallsToDisplay(perspective: Int, notation: String, orientation: QuoridorWallOrientation, groove: Int, segment: Int) throws {
        let view = QuoridorPerspective(perspective: perspective)
        let wall = try #require(QuoridorWall(notation: notation))
        #expect(view.display(wall) == QuoridorDisplayWall(orientation: orientation, groove: groove, segment: segment))
    }

    @Test(arguments: [0, 1])
    func roundTripsDisplayWalls(perspective: Int) {
        let view = QuoridorPerspective(perspective: perspective)
        for wall in Self.allWalls {
            #expect(view.wall(view.display(wall)) == wall)
        }
    }

    @Test(arguments: [0, 1])
    func drawsWallsInTheGrooveBetweenTheirCells(perspective: Int) throws {
        let view = QuoridorPerspective(perspective: perspective)
        let layout = QuoridorBoardLayout(side: 402)
        for wall in Self.allWalls {
            let rect = layout.wallRect(view.display(wall))
            let cells = [(0, 0), (0, 1), (1, 0), (1, 1)].compactMap { QuoridorCell(row: wall.row + $0.0, col: wall.col + $0.1) }
            let rects = cells.map { cell in
                let display = view.display(cell)
                return layout.cellRect(displayRow: display.row, displayCol: display.col)
            }
            let union = rects.dropFirst().reduce(rects[0]) { $0.union($1) }
            #expect(abs(rect.midX - union.midX) < 0.001)
            #expect(abs(rect.midY - union.midY) < 0.001)
            #expect(rects.allSatisfy { !$0.intersects(rect.insetBy(dx: 0.01, dy: 0.01)) })
            switch wall.orientation {
            case .horizontal:
                #expect(abs(rect.width - union.width) < 0.001)
                #expect(abs(rect.height - layout.groove) < 0.001)
            case .vertical:
                #expect(abs(rect.height - union.height) < 0.001)
                #expect(abs(rect.width - layout.groove) < 0.001)
            }
        }
    }

    @Test func layoutFillsTheSide() {
        for side in [CGFloat(402), 700] {
            let layout = QuoridorBoardLayout(side: side)
            let last = layout.cellRect(displayRow: 8, displayCol: 8)
            #expect(abs(last.maxX + layout.padding - side) < 0.001)
            #expect(abs(last.maxY + layout.padding - side) < 0.001)
            #expect(layout.cell > layout.groove * 4)
        }
    }

    @Test func modelSplitsLegalMovesAndLastMove() throws {
        var board = QuoridorBoardState()
        board.pawns = ["e4", "e6"]
        board.walls = ["d6h", "e4h"]
        board.wallsLeft = [8, 9]
        board.lastMove = "e4h"
        board.legalMoves = ["d4", "f4", "e3", "a1h", "c2v", "bogus"]
        let model = QuoridorBoardModel(board, interactive: true)
        #expect(model.pawns == [QuoridorCell(notation: "e4"), QuoridorCell(notation: "e6")])
        #expect(model.walls.map(\.notation) == ["d6h", "e4h"])
        #expect(model.targets == Set(["d4", "f4", "e3"].compactMap { QuoridorCell(notation: $0) }))
        #expect(model.legalWalls == Set(["a1h", "c2v"].compactMap { QuoridorWall(notation: $0) }))
        #expect(model.lastWall == QuoridorWall(notation: "e4h"))
        #expect(model.lastCell == nil)
        #expect(model.wallsLeft(seat: 0) == 8)
        #expect(model.wallsLeft(seat: 1) == 9)
        #expect(model.seat(at: try #require(QuoridorCell(notation: "e6"))) == 1)

        let spectator = QuoridorBoardModel(board, interactive: false)
        #expect(spectator.targets.isEmpty)
        #expect(spectator.legalWalls.isEmpty)

        let empty = QuoridorBoardModel(QuoridorBoardState(), interactive: true)
        #expect(empty.wallsLeft(seat: 0) == 10)
        #expect(empty.lastMove == nil)
    }
}
