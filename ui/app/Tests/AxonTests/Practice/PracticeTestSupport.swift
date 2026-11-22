import Foundation
@testable import Axon

struct ConstantRandom: RandomNumberGenerator {
    let value: UInt64

    init(_ unit: Double) {
        value = UInt64(min(max(unit, 0), 1 - 0x1p-53) * 0x1p53) << 11
    }

    mutating func next() -> UInt64 {
        value
    }
}

struct PracticePlayout<Position> {
    let position: Position
    let plies: Int
    let legal: Bool
}

enum PracticeReplay {
    static func play<R: PracticeRules>(_ rules: R.Type, _ moves: [String], from position: R.Position) -> R.Position? {
        var position = position
        for move in moves {
            guard R.legalMoves(position).contains(move), let next = R.apply(move, to: position) else { return nil }
            position = next
        }
        return position
    }

    static func selfPlay<R: PracticeRules>(
        _ rules: R.Type,
        seed: UInt64,
        levels: [PracticeLevel],
        plyCap: Int = 2000
    ) -> PracticePlayout<R.Position> {
        var position = R.start(seed: seed)
        var random = PracticeRandom(seed: seed)
        var plies = 0
        while R.outcome(position) == nil, plies < plyCap {
            let legal = R.legalMoves(position)
            guard let move = R.botMove(position, level: levels[R.turn(position)], using: &random), legal.contains(move),
                  let next = R.apply(move, to: position)
            else { return PracticePlayout(position: position, plies: plies, legal: false) }
            position = next
            plies += 1
        }
        return PracticePlayout(position: position, plies: plies, legal: true)
    }
}
