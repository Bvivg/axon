import Foundation

extension QuoridorPractice {
    private static let depths: [PracticeLevel: Int] = [.easy: 1, .medium: 2, .hard: 3]
    private static let noise: [PracticeLevel: Double] = [.easy: 2.5, .medium: 0.6, .hard: 0.05]
    private static let win = 1000.0

    private static func evaluate(_ position: Position, seat: Int) -> Double {
        if let winner = position.winner { return winner == seat ? win : -win }
        let mine = distance(position, from: position.pawns[seat], goal: goalRow(seat))
        let theirs = distance(position, from: position.pawns[1 - seat], goal: goalRow(1 - seat))
        let tempo = position.turn == seat ? 0.5 : -0.5
        return Double(theirs - mine) + tempo + 0.15 * Double(position.wallsLeft[seat] - position.wallsLeft[1 - seat])
    }

    private static func candidates(_ position: Position) -> [String] {
        let seat = position.turn
        var out = pawnMoves(position, seat: seat).map(cellName)
        guard position.wallsLeft[seat] > 0 else { return out }
        let critical = critical(position)
        var seen = Set<Wall>()
        for cell in path(position, seat: 1 - seat).prefix(5) {
            for row in (cell / size - 1)...(cell / size) {
                for col in (cell % size - 1)...(cell % size) {
                    for orientation in Orientation.allCases {
                        guard let wall = Wall(row: row, col: col, orientation: orientation), !seen.contains(wall) else { continue }
                        let legal = if let critical {
                            canPlace(position, seat: seat, wall, critical: critical)
                        } else {
                            canPlace(position, seat: seat, wall)
                        }
                        guard legal else { continue }
                        seen.insert(wall)
                        out.append(wall.notation)
                    }
                }
            }
        }
        return out
    }

    private static func search(_ position: Position, depth: Int, alpha: Double, beta: Double) -> Double {
        let seat = position.turn
        if depth == 0 || position.winner != nil { return evaluate(position, seat: seat) }
        var alpha = alpha
        var best = -Double.infinity
        for move in candidates(position) {
            let score = -search(played(position, move), depth: depth - 1, alpha: -beta, beta: -alpha)
            if score > best { best = score }
            if best > alpha { alpha = best }
            if alpha >= beta { break }
        }
        return best
    }

    static func botMove(_ position: Position, level: PracticeLevel, using random: inout some RandomNumberGenerator) -> String? {
        guard outcome(position) == nil else { return nil }
        let depth = depths[level] ?? 1
        let spread = noise[level] ?? 0
        var best: String?
        var bestScore = -Double.infinity
        for move in candidates(position) {
            let next = played(position, move)
            let immediate = next.winner == position.turn ? win * 2 : 0
            let bonus = immediate + (PracticeRandom.unit(&random) - 0.5) * spread + (move.utf8.count == 3 ? -0.3 : 0)
            let needed = bestScore - bonus
            let ceiling = needed.isFinite ? -needed : Double.infinity
            let value = search(next, depth: depth - 1, alpha: -Double.infinity, beta: ceiling)
            if needed.isFinite, value >= -needed { continue }
            let score = bonus - value
            if score > bestScore {
                bestScore = score
                best = move
            }
        }
        return best
    }
}
