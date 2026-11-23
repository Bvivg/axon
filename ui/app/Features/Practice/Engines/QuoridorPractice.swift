import Foundation

enum QuoridorPractice: PracticeRules {
    static let size = 9
    static let anchors = size - 1
    static let wallsEach = 10
    static let maxPlies = 400

    private static let directions = [(1, 0), (-1, 0), (0, 1), (0, -1)]
}

extension QuoridorPractice {
    enum Orientation: Character, Sendable, CaseIterable {
        case horizontal = "h"
        case vertical = "v"
    }

    struct Wall: Sendable, Hashable {
        var row: Int
        var col: Int
        var orientation: Orientation

        init?(row: Int, col: Int, orientation: Orientation) {
            guard (0..<QuoridorPractice.anchors).contains(row), (0..<QuoridorPractice.anchors).contains(col) else { return nil }
            self.row = row
            self.col = col
            self.orientation = orientation
        }

        init?(notation: String) {
            let bytes = Array(notation.utf8)
            guard bytes.count == 3, let orientation = Orientation(rawValue: Character(UnicodeScalar(bytes[2]))) else { return nil }
            self.init(row: Int(bytes[1]) - 49, col: Int(bytes[0]) - 97, orientation: orientation)
        }

        var notation: String {
            QuoridorPractice.cellName(row * QuoridorPractice.size + col) + String(orientation.rawValue)
        }
    }

    struct Position: Sendable, Equatable {
        var pawns: [Int]
        var wallsLeft: [Int]
        var horizontal: UInt64
        var vertical: UInt64
        var turn: Int
        var moves: [String]

        init(
            pawns: [Int] = [4, QuoridorPractice.size * (QuoridorPractice.size - 1) + 4],
            wallsLeft: [Int] = [QuoridorPractice.wallsEach, QuoridorPractice.wallsEach],
            horizontal: UInt64 = 0,
            vertical: UInt64 = 0,
            turn: Int = 0,
            moves: [String] = []
        ) {
            self.pawns = pawns
            self.wallsLeft = wallsLeft
            self.horizontal = horizontal
            self.vertical = vertical
            self.turn = turn
            self.moves = moves
        }

        init?(
            pawns names: [String],
            walls: [String] = [],
            wallsLeft: [Int] = [QuoridorPractice.wallsEach, QuoridorPractice.wallsEach],
            turn: Int = 0
        ) {
            let cells = names.compactMap(QuoridorPractice.cell(named:))
            guard cells.count == 2, wallsLeft.count == 2, (0...1).contains(turn) else { return nil }
            self.init(pawns: cells, wallsLeft: wallsLeft, turn: turn)
            for name in walls {
                guard let wall = Wall(notation: name) else { return nil }
                self = with(wall)
            }
        }

        var pawnNames: [String] { pawns.map(QuoridorPractice.cellName) }
        var lastMove: String? { moves.last }

        var walls: [String] {
            var out: [String] = []
            for orientation in Orientation.allCases {
                for row in 0..<QuoridorPractice.anchors {
                    for col in 0..<QuoridorPractice.anchors where hasWall(row, col, orientation) {
                        if let wall = Wall(row: row, col: col, orientation: orientation) { out.append(wall.notation) }
                    }
                }
            }
            return out.sorted()
        }

        var winner: Int? {
            if pawns[0] / QuoridorPractice.size == QuoridorPractice.goalRow(0) { return 0 }
            if pawns[1] / QuoridorPractice.size == QuoridorPractice.goalRow(1) { return 1 }
            return nil
        }

        func hasWall(_ row: Int, _ col: Int, _ orientation: Orientation) -> Bool {
            let bits = orientation == .horizontal ? horizontal : vertical
            return bits & QuoridorPractice.bit(row, col) != 0
        }

        func with(_ wall: Wall) -> Position {
            var next = self
            switch wall.orientation {
            case .horizontal: next.horizontal |= QuoridorPractice.bit(wall.row, wall.col)
            case .vertical: next.vertical |= QuoridorPractice.bit(wall.row, wall.col)
            }
            return next
        }
    }

    static func goalRow(_ seat: Int) -> Int { seat == 0 ? size - 1 : 0 }

    static func cellName(_ cell: Int) -> String {
        String(UnicodeScalar(UInt8(97 + cell % size))) + String(cell / size + 1)
    }

    static func cell(named name: String) -> Int? {
        let bytes = Array(name.utf8)
        guard bytes.count == 2 else { return nil }
        let col = Int(bytes[0]) - 97
        let row = Int(bytes[1]) - 49
        guard (0..<size).contains(row), (0..<size).contains(col) else { return nil }
        return row * size + col
    }

    static func bit(_ row: Int, _ col: Int) -> UInt64 {
        1 << UInt64(row * anchors + col)
    }

    static func blocked(_ position: Position, _ from: Int, _ to: Int) -> Bool {
        let fr = from / size, fc = from % size, tr = to / size, tc = to % size
        if fc == tc, abs(fr - tr) == 1 {
            let row = min(fr, tr)
            return (fc < anchors && position.horizontal & bit(row, fc) != 0)
                || (fc > 0 && position.horizontal & bit(row, fc - 1) != 0)
        }
        if fr == tr, abs(fc - tc) == 1 {
            let col = min(fc, tc)
            return (fr < anchors && position.vertical & bit(fr, col) != 0)
                || (fr > 0 && position.vertical & bit(fr - 1, col) != 0)
        }
        return true
    }

    private static func step(_ position: Position, _ cell: Int, _ direction: Int) -> Int {
        let row = cell / size + directions[direction].0
        let col = cell % size + directions[direction].1
        guard row >= 0, row < size, col >= 0, col < size else { return -1 }
        let next = row * size + col
        return blocked(position, cell, next) ? -1 : next
    }

    private static func neighbours(_ position: Position, _ cell: Int) -> [Int] {
        (0..<directions.count).map { step(position, cell, $0) }.filter { $0 >= 0 }
    }

    private struct Route {
        let end: Int
        let parent: [Int]
        let distance: Int
    }

    private static func search(_ position: Position, from: Int, goal: Int) -> Route {
        var parent = [Int](repeating: -1, count: size * size)
        if from / size == goal { return Route(end: from, parent: parent, distance: 0) }
        var dist = [Int](repeating: 0, count: size * size)
        var queue = [Int](repeating: 0, count: size * size)
        dist[from] = 1
        queue[0] = from
        var head = 0
        var tail = 1
        while head < tail {
            let current = queue[head]
            head += 1
            for direction in 0..<directions.count {
                let next = step(position, current, direction)
                if next < 0 || dist[next] != 0 { continue }
                dist[next] = dist[current] + 1
                parent[next] = current
                if next / size == goal { return Route(end: next, parent: parent, distance: dist[next] - 1) }
                queue[tail] = next
                tail += 1
            }
        }
        return Route(end: -1, parent: parent, distance: -1)
    }

    static func distance(_ position: Position, from: Int, goal: Int) -> Int {
        search(position, from: from, goal: goal).distance
    }

    static func path(_ position: Position, seat: Int) -> [Int] {
        let from = position.pawns[seat]
        let found = search(position, from: from, goal: goalRow(seat))
        guard found.distance >= 0 else { return [from] }
        var out: [Int] = []
        var at = found.end
        while at != from {
            out.append(at)
            at = found.parent[at]
        }
        out.append(from)
        return out.reversed()
    }

    private static func cuts(_ position: Position, seat: Int) -> (horizontal: UInt64, vertical: UInt64)? {
        let route = path(position, seat: seat)
        guard route.count > 1 || route[0] / size == goalRow(seat) else { return nil }
        var horizontal: UInt64 = 0
        var vertical: UInt64 = 0
        for index in stride(from: 1, to: route.count, by: 1) {
            let previous = route[index - 1], current = route[index]
            if previous % size == current % size {
                let row = min(previous / size, current / size), col = current % size
                if col < anchors { horizontal |= bit(row, col) }
                if col > 0 { horizontal |= bit(row, col - 1) }
            } else {
                let row = current / size, col = min(previous % size, current % size)
                if row < anchors { vertical |= bit(row, col) }
                if row > 0 { vertical |= bit(row - 1, col) }
            }
        }
        return (horizontal, vertical)
    }

    static func pawnMoves(_ position: Position, seat: Int) -> [Int] {
        let me = position.pawns[seat], other = position.pawns[1 - seat]
        var out: [Int] = []
        for next in neighbours(position, me) {
            if next != other {
                out.append(next)
                continue
            }
            let dr = other / size - me / size, dc = other % size - me % size
            let jr = other / size + dr, jc = other % size + dc
            if jr >= 0, jr < size, jc >= 0, jc < size, !blocked(position, other, jr * size + jc) {
                out.append(jr * size + jc)
                continue
            }
            for side in neighbours(position, other) {
                let level = dr != 0 && side / size == other / size
                let file = dc != 0 && side % size == other % size
                if level || file { out.append(side) }
            }
        }
        return out.sorted()
    }

    private static func overlaps(_ position: Position, _ wall: Wall) -> Bool {
        let row = wall.row, col = wall.col
        if position.hasWall(row, col, .horizontal) || position.hasWall(row, col, .vertical) { return true }
        if wall.orientation == .horizontal {
            return (col > 0 && position.hasWall(row, col - 1, .horizontal))
                || (col < anchors - 1 && position.hasWall(row, col + 1, .horizontal))
        }
        return (row > 0 && position.hasWall(row - 1, col, .vertical))
            || (row < anchors - 1 && position.hasWall(row + 1, col, .vertical))
    }

    private static func placeable(_ position: Position, seat: Int, _ wall: Wall) -> Bool {
        position.wallsLeft[seat] > 0 && !overlaps(position, wall)
    }

    static func canPlace(_ position: Position, seat: Int, _ wall: Wall) -> Bool {
        guard placeable(position, seat: seat, wall) else { return false }
        let next = position.with(wall)
        return distance(next, from: next.pawns[0], goal: goalRow(0)) >= 0
            && distance(next, from: next.pawns[1], goal: goalRow(1)) >= 0
    }

    struct Critical {
        let horizontal: UInt64
        let vertical: UInt64

        func covers(_ wall: Wall) -> Bool {
            let bits = wall.orientation == .horizontal ? horizontal : vertical
            return bits & QuoridorPractice.bit(wall.row, wall.col) != 0
        }
    }

    static func critical(_ position: Position) -> Critical? {
        guard let first = cuts(position, seat: 0), let second = cuts(position, seat: 1) else { return nil }
        return Critical(horizontal: first.horizontal | second.horizontal, vertical: first.vertical | second.vertical)
    }

    static func canPlace(_ position: Position, seat: Int, _ wall: Wall, critical: Critical) -> Bool {
        guard placeable(position, seat: seat, wall) else { return false }
        return !critical.covers(wall) || canPlace(position, seat: seat, wall)
    }

}

extension QuoridorPractice {
    static func start(seed: UInt64) -> Position {
        Position()
    }

    static func turn(_ position: Position) -> Int {
        position.turn
    }

    static func outcome(_ position: Position) -> PracticeOutcome? {
        if let winner = position.winner { return .won(seat: winner) }
        return position.moves.count >= maxPlies ? .draw : nil
    }

    static func legalMoves(_ position: Position) -> [String] {
        guard outcome(position) == nil else { return [] }
        return legal(position)
    }

    private static func legal(_ position: Position) -> [String] {
        let seat = position.turn
        var out = pawnMoves(position, seat: seat).map(cellName)
        guard position.wallsLeft[seat] > 0, let critical = critical(position) else { return out }
        for row in 0..<anchors {
            for col in 0..<anchors {
                for orientation in Orientation.allCases {
                    guard let wall = Wall(row: row, col: col, orientation: orientation) else { continue }
                    if canPlace(position, seat: seat, wall, critical: critical) { out.append(wall.notation) }
                }
            }
        }
        return out
    }

    static func apply(_ move: String, to position: Position) -> Position? {
        guard outcome(position) == nil else { return nil }
        let seat = position.turn
        if let wall = Wall(notation: move) {
            guard canPlace(position, seat: seat, wall) else { return nil }
            return played(position, move)
        }
        guard let cell = cell(named: move), pawnMoves(position, seat: seat).contains(cell) else { return nil }
        return played(position, move)
    }

    static func played(_ position: Position, _ move: String) -> Position {
        let seat = position.turn
        var next = position
        if let wall = Wall(notation: move) {
            next = position.with(wall)
            next.wallsLeft[seat] -= 1
        } else if let cell = cell(named: move) {
            next.pawns[seat] = cell
        }
        next.turn = 1 - seat
        next.moves.append(move)
        return next
    }
}
