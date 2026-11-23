import Foundation

enum TicTacToePractice: PracticeRules {
    static let empty = 0
    static let x = 1
    static let o = 2
    static let lines = [[0, 1, 2], [3, 4, 5], [6, 7, 8], [0, 3, 6], [1, 4, 7], [2, 5, 8], [0, 4, 8], [2, 4, 6]]

    struct Position: Sendable, Equatable {
        var cells: [Int]
        var moves: [String]

        init(cells: [Int] = Array(repeating: TicTacToePractice.empty, count: 9), moves: [String] = []) {
            self.cells = cells
            self.moves = moves
        }

        var turn: Int { cells.filter { $0 != TicTacToePractice.empty }.count % 2 }
        var lastMove: String? { moves.last }
        var winningLine: [Int] { TicTacToePractice.line(cells) ?? [] }

        var winner: Int? {
            guard let line = TicTacToePractice.line(cells) else { return nil }
            return cells[line[0]] - 1
        }

        var isFull: Bool { !cells.contains(TicTacToePractice.empty) }
    }

    static func start(seed: UInt64) -> Position {
        Position()
    }

    static func turn(_ position: Position) -> Int {
        position.turn
    }

    static func outcome(_ position: Position) -> PracticeOutcome? {
        if let winner = position.winner { return .won(seat: winner) }
        return position.isFull ? .draw : nil
    }

    static func legalMoves(_ position: Position) -> [String] {
        guard outcome(position) == nil else { return [] }
        return position.cells.indices.filter { position.cells[$0] == empty }.map(String.init)
    }

    static func apply(_ move: String, to position: Position) -> Position? {
        guard outcome(position) == nil, let cell = Int(move), String(cell) == move,
              position.cells.indices.contains(cell), position.cells[cell] == empty
        else { return nil }
        var next = position
        next.cells[cell] = position.turn + 1
        next.moves.append(move)
        return next
    }

    static func botMove(_ position: Position, level: PracticeLevel, using random: inout some RandomNumberGenerator) -> String? {
        guard outcome(position) == nil else { return nil }
        let me = position.turn + 1
        let free = position.cells.indices.filter { position.cells[$0] == empty }
        let wander = switch level {
        case .easy: PracticeRandom.unit(&random) < 0.55
        case .medium: PracticeRandom.unit(&random) < 0.25
        case .hard: false
        }
        if wander { return String(free[PracticeRandom.index(free.count, &random)]) }
        var cells = position.cells
        var best = Int.min
        var choices: [Int] = []
        for cell in free {
            cells[cell] = me
            let score = -negamax(&cells, toMove: 3 - me, depth: 0, alpha: -1000, beta: 1000)
            cells[cell] = empty
            if score > best {
                best = score
                choices = [cell]
            } else if score == best {
                choices.append(cell)
            }
        }
        return String(choices[PracticeRandom.index(choices.count, &random)])
    }

    static func line(_ cells: [Int]) -> [Int]? {
        lines.first { cells[$0[0]] != empty && cells[$0[0]] == cells[$0[1]] && cells[$0[0]] == cells[$0[2]] }
    }

    private static func negamax(_ cells: inout [Int], toMove: Int, depth: Int, alpha: Int, beta: Int) -> Int {
        if line(cells) != nil { return depth - 10 }
        if !cells.contains(empty) { return 0 }
        var alpha = alpha
        var best = Int.min
        for cell in 0..<cells.count where cells[cell] == empty {
            cells[cell] = toMove
            let score = -negamax(&cells, toMove: 3 - toMove, depth: depth + 1, alpha: -beta, beta: -alpha)
            cells[cell] = empty
            best = max(best, score)
            alpha = max(alpha, best)
            if beta <= alpha { break }
        }
        return best
    }
}
