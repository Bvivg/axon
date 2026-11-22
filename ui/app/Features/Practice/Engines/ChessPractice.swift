import Foundation

enum ChessPractice: PracticeRules {
    static let maxPlies = 600
    static let mate = 100_000
    private static let infinity = 1_000_000_000
    private static let values = [0, 100, 320, 330, 500, 900, 0]
    private static let depths: [PracticeLevel: Int] = [.easy: 1, .medium: 2, .hard: 3]
    private static let noise: [PracticeLevel: Double] = [.easy: 120, .medium: 25, .hard: 0]

    private static let pawnTable = [
        0, 0, 0, 0, 0, 0, 0, 0,
        50, 50, 50, 50, 50, 50, 50, 50,
        10, 10, 20, 30, 30, 20, 10, 10,
        5, 5, 10, 25, 25, 10, 5, 5,
        0, 0, 0, 20, 20, 0, 0, 0,
        5, -5, -10, 0, 0, -10, -5, 5,
        5, 10, 10, -20, -20, 10, 10, 5,
        0, 0, 0, 0, 0, 0, 0, 0,
    ]

    private static let knightTable = [
        -50, -40, -30, -30, -30, -30, -40, -50,
        -40, -20, 0, 0, 0, 0, -20, -40,
        -30, 0, 10, 15, 15, 10, 0, -30,
        -30, 5, 15, 20, 20, 15, 5, -30,
        -30, 0, 15, 20, 20, 15, 0, -30,
        -30, 5, 10, 15, 15, 10, 5, -30,
        -40, -20, 0, 5, 5, 0, -20, -40,
        -50, -40, -30, -30, -30, -30, -40, -50,
    ]

    private static let centreTable = [
        -20, -10, -10, -10, -10, -10, -10, -20,
        -10, 0, 0, 0, 0, 0, 0, -10,
        -10, 0, 5, 10, 10, 5, 0, -10,
        -10, 5, 5, 10, 10, 5, 5, -10,
        -10, 0, 10, 10, 10, 10, 0, -10,
        -10, 10, 10, 10, 10, 10, 10, -10,
        -10, 5, 0, 0, 0, 0, 5, -10,
        -20, -10, -10, -10, -10, -10, -10, -20,
    ]

    private static let squareValues: [[Int]] = (0..<16).map { piece in
        let kind = piece & 7
        guard kind != Kind.none, kind <= Kind.king else { return Array(repeating: 0, count: 64) }
        let table: [Int]? = switch kind {
        case Kind.pawn: pawnTable
        case Kind.knight: knightTable
        case Kind.bishop, Kind.queen: centreTable
        default: nil
        }
        return (0..<64).map { square in
            let rank = square / 8, file = square % 8
            let index = piece >> 3 == 0 ? (7 - rank) * 8 + file : rank * 8 + file
            return values[kind] + (table?[index] ?? 0)
        }
    }

    struct Position: Sendable, Equatable {
        var board: Board
        var moves: [String]
        var san: [String]
        var positions: [String]
        var end: End?

        init(board: Board) {
            self.board = board
            moves = []
            san = []
            positions = [board.repetitionKey]
            end = board.end(positions: positions)
        }

        init?(fen: String) {
            guard let board = Board(fen: fen) else { return nil }
            self.init(board: board)
        }

        var fen: String { board.fen }
        var turn: Int { board.turn }
        var inCheck: Bool { board.inCheck }
        var lastMove: String? { moves.last }
    }

    static func start(seed: UInt64) -> Position {
        Position(fen: startFEN)!
    }

    static func turn(_ position: Position) -> Int {
        position.board.turn
    }

    static func outcome(_ position: Position) -> PracticeOutcome? {
        switch position.end {
        case nil: nil
        case .checkmate: .won(seat: position.board.turn ^ 1)
        default: .draw
        }
    }

    static func legalMoves(_ position: Position) -> [String] {
        guard position.end == nil else { return [] }
        return position.board.legal.map(\.uci)
    }

    static func apply(_ move: String, to position: Position) -> Position? {
        guard position.end == nil, let wanted = Move(uci: move), position.board.legal.contains(wanted) else { return nil }
        var next = position
        next.san.append(position.board.san(wanted))
        next.board = position.board.played(wanted)
        next.moves.append(wanted.uci)
        if next.board.halfmove == 0 { next.positions = [] }
        next.positions.append(next.board.repetitionKey)
        next.end = next.board.end(positions: next.positions) ?? (next.moves.count >= maxPlies ? .moveLimit : nil)
        return next
    }

    static func botMove(_ position: Position, level: PracticeLevel, using random: inout some RandomNumberGenerator) -> String? {
        guard position.end == nil else { return nil }
        var board = position.board
        let moves = ordered(board.legalMoves(), on: board)
        guard var best = moves.first else { return nil }
        let depth = depths[level] ?? 1
        let spread = noise[level] ?? 0
        var bestScore = -Double.infinity
        for move in moves {
            let jitter = spread > 0 ? (PracticeRandom.unit(&random) - 0.5) * spread : 0
            let needed = bestScore - jitter
            let floor = needed.isFinite ? Int(needed.rounded(.down)) : -infinity
            let undo = board.make(move)
            let value = search(&board, depth: depth - 1, alpha: -infinity, beta: -floor, ply: 1)
            board.unmake(move, undo)
            if needed.isFinite, value >= -floor { continue }
            let score = Double(-value) + jitter
            if score > bestScore {
                bestScore = score
                best = move
            }
        }
        return best.uci
    }

    static func evaluate(_ board: Board) -> Int {
        var score = 0
        for square in 0..<64 {
            let piece = Int(board.squares[square])
            guard piece != 0 else { continue }
            let value = squareValues[piece][square]
            score += piece >> 3 == 0 ? value : -value
        }
        return board.turn == 0 ? score : -score
    }

    private static func ordered(_ moves: [Move], on board: Board) -> [Move] {
        let keys = moves.map { move in
            let captured = board.captured(move)
            let attacker = Int(board.squares[move.from]) & 7
            return (captured != Kind.none ? values[captured] * 10 - values[attacker] : 0) + (move.promo != Kind.none ? 800 : 0)
        }
        return moves.indices.sorted { keys[$0] != keys[$1] ? keys[$0] > keys[$1] : $0 < $1 }.map { moves[$0] }
    }

    private static func search(_ board: inout Board, depth: Int, alpha: Int, beta: Int, ply: Int) -> Int {
        if depth == 0 {
            if !board.hasLegalMove() { return board.inCheck ? -mate + ply : 0 }
            if board.insufficientMaterial || board.halfmove >= 100 { return 0 }
            return evaluate(board)
        }
        let moves = board.legalMoves()
        if moves.isEmpty { return board.inCheck ? -mate + ply : 0 }
        if board.insufficientMaterial || board.halfmove >= 100 { return 0 }
        var alpha = alpha
        var best = -infinity
        for move in ordered(moves, on: board) {
            let undo = board.make(move)
            let score = -search(&board, depth: depth - 1, alpha: -beta, beta: -alpha, ply: ply + 1)
            board.unmake(move, undo)
            if score > best { best = score }
            if score > alpha { alpha = score }
            if alpha >= beta { break }
        }
        return best
    }
}
