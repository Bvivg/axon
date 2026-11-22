import Foundation

typealias Tournament = Axon_Tournament_V1_Tournament
typealias TournamentFormat = Axon_Tournament_V1_TournamentFormat
typealias TournamentStatus = Axon_Tournament_V1_TournamentStatus
typealias TournamentMatch = Axon_Tournament_V1_Match
typealias TournamentRound = Axon_Tournament_V1_Round
typealias TournamentStanding = Axon_Tournament_V1_Standing
typealias TournamentRegistration = Axon_Tournament_V1_Registration
typealias GameAnalysis = Axon_Analysis_V1_GetAnalysisResponse
typealias PositionAnalysis = Axon_Analysis_V1_PositionAnalysis
typealias MoveClass = Axon_Analysis_V1_MoveClass

struct NewTournament: Equatable, Sendable {
    var name: String
    var game: GameKind
    var format: TournamentFormat
    var capacity: Int
    var rounds: Int?
}

enum TournamentAction: String, Equatable, Sendable {
    case register
    case withdraw
    case start
    case cancel
}

extension GameKind {
    static let tournamentGames: [GameKind] = allCases.filter { $0 != .durak }
}

extension TournamentFormat {
    static let offered: [TournamentFormat] = [.singleElimination, .swiss]

    var title: String { self == .swiss ? "Swiss" : "Single elimination" }
}

extension TournamentStatus {
    var title: String {
        switch self {
        case .registration: "Registration"
        case .seeding: "Seeding"
        case .running: "In progress"
        case .finished: "Finished"
        case .cancelled: "Cancelled"
        default: "Unavailable"
        }
    }

    var isLive: Bool { self == .running || self == .seeding }
    var isOver: Bool { self == .finished || self == .cancelled }
}

extension Tournament {
    var kind: GameKind { GameKind(game: game) }

    var summary: String {
        "\(kind.title) · \(format.title) · \(playerCount)/\(capacity) players"
    }

    func roundTitle(_ number: Int) -> String {
        let total = Int(totalRounds)
        guard format != .swiss else { return "Round \(number)" }
        switch total - number {
        case 0: return "Final"
        case 1: return "Semifinals"
        case 2: return "Quarterfinals"
        default: return "Round \(number)"
        }
    }

    var seeds: [String: Int] {
        Dictionary(registrations.filter(\.hasSeed).map { ($0.userID, Int($0.seed)) }) { first, _ in first }
    }

    func isRegistered(_ user: String) -> Bool {
        registrations.contains { $0.userID == user && !$0.returned }
    }
}

extension TournamentMatch {
    var resultLabel: String {
        switch result {
        case .bye: "Bye"
        case .aborted: "Aborted"
        case .draw: "Draw"
        case .win: forfeit ? "No-show" : "Played"
        default: "Playing"
        }
    }
}

enum TournamentPoints {
    static func label(_ half: UInt32) -> String {
        let whole = half / 2
        guard half % 2 == 1 else { return "\(whole)" }
        return whole > 0 ? "\(whole)½" : "½"
    }
}

enum MoveMark: String, Sendable {
    case inaccuracy = "?!"
    case mistake = "?"
    case blunder = "??"
}

extension MoveClass {
    var mark: MoveMark? {
        switch self {
        case .inaccuracy: .inaccuracy
        case .mistake: .mistake
        case .blunder: .blunder
        default: nil
        }
    }

    var title: String {
        switch self {
        case .best: "Best move"
        case .good: "Good move"
        case .inaccuracy: "Inaccuracy"
        case .mistake: "Mistake"
        case .blunder: "Blunder"
        default: ""
        }
    }
}

extension PositionAnalysis {
    private static let winSlope = 0.00368208

    var evaluationLabel: String {
        if hasMate {
            if mate == 0 { return centipawns > 0 ? "1-0" : "0-1" }
            return "\(mate > 0 ? "+" : "−")M\(abs(mate))"
        }
        if centipawns == 0 { return "0.0" }
        let tenths = (abs(Int(centipawns)) + 5) / 10
        return "\(centipawns > 0 ? "+" : "−")\(tenths / 10).\(tenths % 10)"
    }

    var whiteShare: Double {
        if hasMate { return centipawns > 0 ? 1 : 0 }
        let clamped = Double(max(-1500, min(1500, centipawns)))
        return 1 / (1 + exp(-Self.winSlope * clamped))
    }
}
