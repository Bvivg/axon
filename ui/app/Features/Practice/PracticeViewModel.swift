import Foundation
import Observation

enum PracticeOpponent: Hashable, Sendable {
    case computer(PracticeLevel)
    case local

    var title: String {
        switch self {
        case .computer(let level): level.title
        case .local: "Two players"
        }
    }
}

extension PracticeLevel {
    var title: String { rawValue.capitalized }
}

protocol PracticeGame: PracticeRules {
    static var kind: GameKind { get }
    static var opponents: [PracticeOpponent] { get }
    static var choosesSide: Bool { get }
    static func session(_ position: Position, legal: [String], viewer: Int?) -> Session
    static func moves(_ position: Position) -> [String]
    static func ending(_ position: Position) -> String?
    static func hint(_ position: Position, seat: Int) -> String?
}

extension PracticeGame {
    static var opponents: [PracticeOpponent] {
        PracticeLevel.allCases.map(PracticeOpponent.computer) + [.local]
    }

    static var choosesSide: Bool { true }

    static func hint(_ position: Position, seat: Int) -> String? { nil }
}

@MainActor
@Observable
final class PracticeViewModel<Game: PracticeGame> {
    private(set) var history: [Game.Position]
    private(set) var opponent: PracticeOpponent
    private(set) var seat = 0
    private(set) var thinking = false
    var dismissed = false
    @ObservationIgnored private var random: PracticeRandom
    @ObservationIgnored private var bot: Task<Void, Never>?
    @ObservationIgnored let botDelay: Duration

    init(seed: UInt64 = .random(in: .min ... .max), botDelay: Duration = .milliseconds(450)) {
        random = PracticeRandom(seed: seed)
        history = [Game.start(seed: seed)]
        opponent = Game.opponents.first ?? .local
        self.botDelay = botDelay
    }

    var position: Game.Position { history[history.count - 1] }
    var turn: Int { Game.turn(position) }
    var outcome: PracticeOutcome? { Game.outcome(position) }
    var isLocal: Bool { opponent == .local }
    var botTurn: Bool { !isLocal && outcome == nil && turn != seat }
    var legal: [String] { outcome != nil || botTurn ? [] : Game.legalMoves(position) }
    var interactive: Bool { outcome == nil && !botTurn }
    var perspective: Int { isLocal ? (Game.kind == .ticTacToe ? 0 : turn) : seat }
    var session: Session { Game.session(position, legal: legal, viewer: isLocal ? nil : seat) }
    var moves: [String] { Game.moves(position) }

    var canUndo: Bool {
        guard !thinking, history.count > 1 else { return false }
        return isLocal || history.dropLast().contains { Game.turn($0) == seat }
    }

    func name(_ seat: Int) -> String {
        let side = Game.kind.seatNames[seat]
        if isLocal { return side }
        return seat == self.seat ? "You" : "Computer"
    }

    var title: String? {
        guard let outcome else { return nil }
        switch outcome {
        case .draw:
            return "Draw"
        case .won(let winner):
            if isLocal { return "\(Game.kind.seatNames[winner]) wins" }
            return winner == seat ? "You won" : "You lost"
        }
    }

    var status: String {
        if let title {
            return Game.ending(position).map { "\(title) · \($0)" } ?? title
        }
        if thinking || botTurn { return "Computer is thinking" }
        let lead = isLocal ? "\(Game.kind.seatNames[turn]) to move" : "Your move"
        return Game.hint(position, seat: turn).map { "\(lead) · \($0)" } ?? lead
    }

    func play(_ move: String) {
        guard interactive, legal.contains(move), let next = Game.apply(move, to: position) else { return }
        history.append(next)
        dismissed = false
        scheduleBot()
    }

    func undo() {
        guard canUndo else { return }
        bot?.cancel()
        thinking = false
        var next = history
        repeat {
            next.removeLast()
        } while !isLocal && next.count > 1 && Game.turn(next[next.count - 1]) != seat
        history = next
        dismissed = false
        scheduleBot()
    }

    func restart(opponent next: PracticeOpponent? = nil, seat side: Int? = nil) {
        bot?.cancel()
        thinking = false
        if let next { opponent = next }
        if let side { seat = side }
        let seed = random.next()
        history = [Game.start(seed: seed)]
        dismissed = false
        scheduleBot()
    }

    func start() {
        scheduleBot()
    }

    func stop() {
        bot?.cancel()
        thinking = false
    }

    private func scheduleBot() {
        guard botTurn, case .computer(let level) = opponent else { return }
        let before = position
        let seed = random.next()
        thinking = true
        let delay = botDelay
        bot = Task { [weak self] in
            let started = ContinuousClock.now
            let move = await Task.detached(priority: .userInitiated) {
                var generator = PracticeRandom(seed: seed)
                return Game.botMove(before, level: level, using: &generator)
            }.value
            let elapsed = ContinuousClock.now - started
            if elapsed < delay { try? await Task.sleep(for: delay - elapsed) }
            guard let self, !Task.isCancelled, self.position == before else { return }
            self.thinking = false
            guard let move, let next = Game.apply(move, to: before) else { return }
            self.history.append(next)
            self.scheduleBot()
        }
    }
}
