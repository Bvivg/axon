import Foundation

enum Checkers {
    static let size = 8
    static let squares = 32
    static let maxNotation = 38
    static let maxPathSquares = 13
}

struct CheckersSquare: Sendable, Hashable, Comparable {
    let index: Int

    init?(index: Int) {
        guard (0..<Checkers.squares).contains(index) else { return nil }
        self.index = index
    }

    init?(number: Int) {
        self.init(index: number - 1)
    }

    init?(row: Int, col: Int) {
        guard (0..<Checkers.size).contains(row), (0..<Checkers.size).contains(col), (row + col) % 2 == 1 else { return nil }
        self.init(index: row * 4 + col / 2)
    }

    var number: Int { index + 1 }
    var row: Int { index / 4 }
    var col: Int { 2 * (index % 4) + 1 - row % 2 }

    static func < (lhs: CheckersSquare, rhs: CheckersSquare) -> Bool {
        lhs.index < rhs.index
    }

    static func at(displayRow: Int, displayCol: Int, perspective: Int) -> CheckersSquare? {
        let last = Checkers.size - 1
        return perspective == 1
            ? CheckersSquare(row: displayRow, col: displayCol)
            : CheckersSquare(row: last - displayRow, col: last - displayCol)
    }

    func display(perspective: Int) -> (row: Int, col: Int) {
        let last = Checkers.size - 1
        return perspective == 1 ? (row, col) : (last - row, last - col)
    }
}

struct CheckersMove: Sendable, Hashable {
    let path: [CheckersSquare]
    let jump: Bool

    init?(path: [CheckersSquare], jump: Bool) {
        guard path.count >= 2, path.count <= Checkers.maxPathSquares, jump || path.count == 2 else { return nil }
        self.path = path
        self.jump = jump
    }

    init?(notation: String) {
        guard (3...Checkers.maxNotation).contains(notation.utf8.count) else { return nil }
        let jump = notation.contains("x")
        guard jump != notation.contains("-") else { return nil }
        let parts = notation.split(separator: jump ? "x" : "-", omittingEmptySubsequences: false)
        var path: [CheckersSquare] = []
        for part in parts {
            guard let square = Self.square(part) else { return nil }
            path.append(square)
        }
        self.init(path: path, jump: jump)
    }

    var notation: String {
        path.map { String($0.number) }.joined(separator: jump ? "x" : "-")
    }

    var origin: CheckersSquare { path[0] }
    var destination: CheckersSquare { path[path.count - 1] }

    var spokenName: String {
        let squares = path.map { String($0.number) }.joined(separator: " to ")
        return jump ? "Capture \(squares)" : "Move \(squares)"
    }

    private static func square(_ part: Substring) -> CheckersSquare? {
        let digits = Array(part.utf8)
        guard (1...2).contains(digits.count),
              digits.allSatisfy({ (48...57).contains($0) }),
              digits[0] != 48,
              let number = Int(part)
        else { return nil }
        return CheckersSquare(number: number)
    }
}
