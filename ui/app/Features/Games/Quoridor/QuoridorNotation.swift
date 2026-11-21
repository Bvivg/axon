import Foundation

enum Quoridor {
    static let size = 9
    static let anchors = size - 1
    static let wallsEach = 10

    static func cellName(row: Int, col: Int) -> String {
        String(UnicodeScalar(UInt8(97 + col))) + String(row + 1)
    }
}

struct QuoridorCell: Sendable, Hashable {
    let row: Int
    let col: Int

    init?(row: Int, col: Int) {
        guard (0..<Quoridor.size).contains(row), (0..<Quoridor.size).contains(col) else { return nil }
        self.row = row
        self.col = col
    }

    init?<S: StringProtocol>(notation: S) {
        let bytes = Array(notation.utf8)
        guard bytes.count == 2 else { return nil }
        self.init(row: Int(bytes[1]) - 49, col: Int(bytes[0]) - 97)
    }

    var notation: String {
        Quoridor.cellName(row: row, col: col)
    }

    var fileName: String { String(notation.prefix(1)) }
    var rankName: String { String(row + 1) }
}

enum QuoridorWallOrientation: Character, Sendable, Hashable, CaseIterable {
    case horizontal = "h"
    case vertical = "v"
}

struct QuoridorWall: Sendable, Hashable {
    let row: Int
    let col: Int
    let orientation: QuoridorWallOrientation

    init?(row: Int, col: Int, orientation: QuoridorWallOrientation) {
        guard (0..<Quoridor.anchors).contains(row), (0..<Quoridor.anchors).contains(col) else { return nil }
        self.row = row
        self.col = col
        self.orientation = orientation
    }

    init?<S: StringProtocol>(notation: S) {
        guard notation.count == 3,
              let anchor = QuoridorCell(notation: notation.prefix(2)),
              let last = notation.last,
              let orientation = QuoridorWallOrientation(rawValue: last)
        else { return nil }
        self.init(row: anchor.row, col: anchor.col, orientation: orientation)
    }

    var notation: String {
        Quoridor.cellName(row: row, col: col) + String(orientation.rawValue)
    }
}

enum QuoridorMove: Sendable, Hashable {
    case pawn(QuoridorCell)
    case wall(QuoridorWall)

    init?(notation: String) {
        if let cell = QuoridorCell(notation: notation) {
            self = .pawn(cell)
        } else if let wall = QuoridorWall(notation: notation) {
            self = .wall(wall)
        } else {
            return nil
        }
    }

    var notation: String {
        switch self {
        case .pawn(let cell): cell.notation
        case .wall(let wall): wall.notation
        }
    }
}

struct QuoridorDisplayWall: Sendable, Hashable {
    let orientation: QuoridorWallOrientation
    let groove: Int
    let segment: Int
}

struct QuoridorPerspective: Sendable, Hashable {
    let flipped: Bool

    init(perspective: Int) {
        flipped = perspective == 1
    }

    func display(_ cell: QuoridorCell) -> (row: Int, col: Int) {
        let last = Quoridor.size - 1
        return flipped ? (cell.row, last - cell.col) : (last - cell.row, cell.col)
    }

    func cell(displayRow: Int, displayCol: Int) -> QuoridorCell? {
        let last = Quoridor.size - 1
        return flipped
            ? QuoridorCell(row: displayRow, col: last - displayCol)
            : QuoridorCell(row: last - displayRow, col: displayCol)
    }

    func display(_ wall: QuoridorWall) -> QuoridorDisplayWall {
        let corners = [(wall.row, wall.col), (wall.row + 1, wall.col + 1)].compactMap { QuoridorCell(row: $0.0, col: $0.1) }.map(display)
        let top = corners.map(\.row).min() ?? 0
        let left = corners.map(\.col).min() ?? 0
        switch wall.orientation {
        case .horizontal: return QuoridorDisplayWall(orientation: .horizontal, groove: top, segment: left)
        case .vertical: return QuoridorDisplayWall(orientation: .vertical, groove: left, segment: top)
        }
    }

    func wall(_ display: QuoridorDisplayWall) -> QuoridorWall? {
        let rows: [Int]
        let cols: [Int]
        switch display.orientation {
        case .horizontal:
            rows = [display.groove, display.groove + 1]
            cols = [display.segment, display.segment + 1]
        case .vertical:
            rows = [display.segment, display.segment + 1]
            cols = [display.groove, display.groove + 1]
        }
        let cells = rows.flatMap { row in cols.compactMap { cell(displayRow: row, displayCol: $0) } }
        guard cells.count == 4, let row = cells.map(\.row).min(), let col = cells.map(\.col).min() else { return nil }
        return QuoridorWall(row: row, col: col, orientation: display.orientation)
    }
}
