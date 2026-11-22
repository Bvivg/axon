import Foundation

enum PracticeLevel: String, CaseIterable, Sendable {
    case easy
    case medium
    case hard
}

enum PracticeOutcome: Equatable, Sendable {
    case won(seat: Int)
    case draw
}

protocol PracticeRules: Sendable {
    associatedtype Position: Sendable, Equatable
    static func start(seed: UInt64) -> Position
    static func legalMoves(_ position: Position) -> [String]
    static func apply(_ move: String, to position: Position) -> Position?
    static func turn(_ position: Position) -> Int
    static func outcome(_ position: Position) -> PracticeOutcome?
    static func botMove(_ position: Position, level: PracticeLevel, using random: inout some RandomNumberGenerator) -> String?
}

struct PracticeRandom: RandomNumberGenerator, Sendable {
    private var state: UInt64

    init(seed: UInt64) {
        state = seed
    }

    mutating func next() -> UInt64 {
        state &+= 0x9E37_79B9_7F4A_7C15
        var mixed = state
        mixed = (mixed ^ (mixed >> 30)) &* 0xBF58_476D_1CE4_E5B9
        mixed = (mixed ^ (mixed >> 27)) &* 0x94D0_49BB_1331_11EB
        return mixed ^ (mixed >> 31)
    }

    static func unit(_ random: inout some RandomNumberGenerator) -> Double {
        Double(random.next() >> 11) * 0x1p-53
    }

    static func index(_ count: Int, _ random: inout some RandomNumberGenerator) -> Int {
        min(count - 1, Int(unit(&random) * Double(count)))
    }
}
