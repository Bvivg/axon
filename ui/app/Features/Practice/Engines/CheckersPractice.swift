import Foundation

enum CheckersPractice: PracticeRules {
    static let squareCount = 32
    static let size = 8
    static let menEach = 12
    static let quietLimit = 80
    static let maxPlies = 500
    static let maxPath = 13
    static let maxNotation = 38

    enum Piece: Int, Sendable, CaseIterable {
        case empty = 0
        case blackMan = 1
        case blackKing = 2
        case whiteMan = 3
        case whiteKing = 4

        init?(letter: Character) {
            guard let piece = Self.allCases.first(where: { $0.letter == letter }) else { return nil }
            self = piece
        }

        var letter: Character {
            switch self {
            case .empty: "."
            case .blackMan: "b"
            case .blackKing: "B"
            case .whiteMan: "w"
            case .whiteKing: "W"
            }
        }

        var side: Int? {
            switch self {
            case .blackMan, .blackKing: 0
            case .whiteMan, .whiteKing: 1
            case .empty: nil
            }
        }

        var isKing: Bool { self == .blackKing || self == .whiteKing }

        var crowned: Piece {
            switch self {
            case .blackMan: .blackKing
            case .whiteMan: .whiteKing
            default: self
            }
        }
    }

    enum End: String, Sendable {
        case noMoves = "no_moves"
        case fortyMoves = "forty_moves"
        case moveLimit = "move_limit"
    }

    struct Move: Sendable, Equatable {
        var path: [Int]
        var jump: Bool

        var from: Int { path[0] }
        var to: Int { path[path.count - 1] }
        var captured: Int { jump ? path.count - 1 : 0 }

        var notation: String {
            path.map { String($0 + 1) }.joined(separator: jump ? "x" : "-")
        }

        init(path: [Int], jump: Bool) {
            self.path = path
            self.jump = jump
        }

        init?(notation raw: String) {
            guard (3...CheckersPractice.maxNotation).contains(raw.utf8.count) else { return nil }
            let jump = raw.contains("x")
            guard jump != raw.contains("-") else { return nil }
            let parts = raw.split(separator: jump ? "x" : "-", omittingEmptySubsequences: false)
            guard parts.count >= 2, parts.count <= CheckersPractice.maxPath, jump || parts.count == 2 else { return nil }
            var path: [Int] = []
            for part in parts {
                let digits = Array(part.utf8)
                guard (1...2).contains(digits.count), digits.allSatisfy({ (48...57).contains($0) }), digits[0] != 48,
                      let number = Int(part), (1...CheckersPractice.squareCount).contains(number)
                else { return nil }
                path.append(number - 1)
            }
            self.init(path: path, jump: jump)
        }
    }

    struct Position: Sendable, Equatable {
        var squares: [Piece]
        var turn: Int
        var quiet: Int
        var ply: Int
        var moves: [String]
        var end: End?

        init(
            squares: [Piece] = CheckersPractice.startSquares(),
            turn: Int = 0,
            quiet: Int = 0,
            ply: Int = 0,
            moves: [String] = [],
            end: End? = nil
        ) {
            self.squares = squares
            self.turn = turn
            self.quiet = quiet
            self.ply = ply
            self.moves = moves
            self.end = end
        }

        init?(board: String, turn: Int = 0, quiet: Int = 0, ply: Int = 0) {
            let pieces = board.compactMap(Piece.init(letter:))
            guard board.count == CheckersPractice.squareCount, pieces.count == CheckersPractice.squareCount else { return nil }
            self.init(squares: pieces, turn: turn, quiet: quiet, ply: ply)
        }

        var board: String { String(squares.map(\.letter)) }
        var lastMove: String? { moves.last }

        func count(_ side: Int) -> Int {
            squares.filter { $0.side == side }.count
        }
    }

    static func startSquares() -> [Piece] {
        (0..<squareCount).map { $0 < menEach ? .blackMan : $0 >= squareCount - menEach ? .whiteMan : .empty }
    }

    static func row(_ square: Int) -> Int { square / 4 }

    static func col(_ square: Int) -> Int { 2 * (square % 4) + 1 - row(square) % 2 }

    static func square(row: Int, col: Int) -> Int? {
        guard (0..<size).contains(row), (0..<size).contains(col), (row + col) % 2 == 1 else { return nil }
        return row * 4 + col / 2
    }

    static func crownRow(_ side: Int) -> Int { side == 0 ? size - 1 : 0 }

    private static let diagonals = [(1, -1), (1, 1), (-1, -1), (-1, 1)]

    private static let neighbours: [[Int]] = (0..<squareCount).map { square in
        diagonals.map { CheckersPractice.square(row: row(square) + $0.0, col: col(square) + $0.1) ?? -1 }
    }

    private static let landings: [[Int]] = (0..<squareCount).map { square in
        diagonals.map { CheckersPractice.square(row: row(square) + 2 * $0.0, col: col(square) + 2 * $0.1) ?? -1 }
    }

    private static func directions(_ piece: Piece) -> [Int] {
        if piece.isKing { return [0, 1, 2, 3] }
        return piece.side == 0 ? [0, 1] : [2, 3]
    }

    static func moves(_ squares: [Piece], side: Int) -> [Move] {
        let jumps = jumps(squares, side: side)
        return jumps.isEmpty ? steps(squares, side: side) : jumps
    }

    private static func steps(_ squares: [Piece], side: Int) -> [Move] {
        var out: [Move] = []
        for (square, piece) in squares.enumerated() where piece.side == side {
            for direction in directions(piece) {
                let to = neighbours[square][direction]
                if to >= 0, squares[to] == .empty { out.append(Move(path: [square, to], jump: false)) }
            }
        }
        return out
    }

    private static func jumps(_ squares: [Piece], side: Int) -> [Move] {
        var out: [Move] = []
        for (square, piece) in squares.enumerated() where piece.side == side {
            var rest = squares
            rest[square] = .empty
            extend(rest, piece: piece, path: [square], out: &out)
        }
        return out
    }

    private static func extend(_ squares: [Piece], piece: Piece, path: [Int], out: inout [Move]) {
        let at = path[path.count - 1]
        guard let side = piece.side else { return }
        var found = false
        for direction in directions(piece) {
            let over = neighbours[at][direction]
            let to = landings[at][direction]
            guard to >= 0, squares[to] == .empty, squares[over].side == 1 - side else { continue }
            found = true
            var next = squares
            next[over] = .empty
            let route = path + [to]
            if !piece.isKing, row(to) == crownRow(side) {
                out.append(Move(path: route, jump: true))
                continue
            }
            extend(next, piece: piece, path: route, out: &out)
        }
        if !found, path.count > 1 { out.append(Move(path: path, jump: true)) }
    }

    private static func between(_ from: Int, _ to: Int) -> Int {
        for direction in 0..<diagonals.count where landings[from][direction] == to {
            return neighbours[from][direction]
        }
        return -1
    }

    static func play(_ squares: [Piece], _ move: Move) -> [Piece] {
        var next = squares
        var piece = next[move.from]
        next[move.from] = .empty
        if move.jump {
            for index in 1..<move.path.count {
                let over = between(move.path[index - 1], move.path[index])
                if over >= 0 { next[over] = .empty }
            }
        }
        if let side = piece.side, !piece.isKing, row(move.to) == crownRow(side) { piece = piece.crowned }
        next[move.to] = piece
        return next
    }

    static func start(seed: UInt64) -> Position {
        Position()
    }

    static func turn(_ position: Position) -> Int {
        position.turn
    }

    static func outcome(_ position: Position) -> PracticeOutcome? {
        switch position.end {
        case nil: nil
        case .noMoves: .won(seat: 1 - position.turn)
        case .fortyMoves, .moveLimit: .draw
        }
    }

    static func legalMoves(_ position: Position) -> [String] {
        guard position.end == nil else { return [] }
        return moves(position.squares, side: position.turn).map(\.notation)
    }

    static func apply(_ move: String, to position: Position) -> Position? {
        guard position.end == nil,
              let chosen = moves(position.squares, side: position.turn).first(where: { $0.notation == move })
        else { return nil }
        return played(position, chosen)
    }

    private static func played(_ position: Position, _ move: Move) -> Position {
        let seat = position.turn
        let quiet = move.jump || !position.squares[move.from].isKing ? 0 : position.quiet + 1
        let squares = play(position.squares, move)
        let ply = position.ply + 1
        var end: End?
        if moves(squares, side: 1 - seat).isEmpty {
            end = .noMoves
        } else if quiet >= quietLimit {
            end = .fortyMoves
        } else if ply >= maxPlies {
            end = .moveLimit
        }
        return Position(squares: squares, turn: 1 - seat, quiet: quiet, ply: ply, moves: position.moves + [move.notation], end: end)
    }

    static func botMove(_ position: Position, level: PracticeLevel, using random: inout some RandomNumberGenerator) -> String? {
        guard position.end == nil else { return nil }
        var best: String?
        var bestScore = -Double.infinity
        for move in moves(position.squares, side: position.turn) {
            let next = played(position, move)
            let won = outcome(next) == .won(seat: position.turn)
            let crowns = !position.squares[move.from].isKing && row(move.to) == crownRow(position.turn)
            let threat = moves(next.squares, side: next.turn).map(\.captured).max() ?? 0
            let gain = (won ? 1000 : 0) + 3 * Double(move.captured) + (crowns ? 2 : 0) - 3 * Double(threat)
            let score = gain + PracticeRandom.unit(&random) * 0.5
            if score > bestScore {
                bestScore = score
                best = move.notation
            }
        }
        return best
    }
}
