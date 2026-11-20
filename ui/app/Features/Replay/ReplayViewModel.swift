import Foundation
import Observation

@MainActor
@Observable
final class ReplayViewModel {
    let sessionID: String
    private(set) var session: Session?
    private(set) var frames: [HistoryFrame] = []
    private(set) var error: String?
    private(set) var isLoading = false
    var index = 0 {
        didSet {
            let clamped = min(max(index, 0), lastIndex)
            if clamped != index { index = clamped }
        }
    }
    @ObservationIgnored private let game: any GameAPI

    init(sessionID: String, game: any GameAPI) {
        self.sessionID = sessionID
        self.game = game
    }

    var kind: GameKind { session?.kind ?? .ticTacToe }
    var lastIndex: Int { max(frames.count - 1, 0) }
    var frame: HistoryFrame? { frames.indices.contains(index) ? frames[index] : nil }
    var lastPly: UInt32 { frames.last?.ply ?? 0 }
    var atStart: Bool { index == 0 }
    var atEnd: Bool { index >= lastIndex }

    var positionText: String {
        guard let frame, frame.ply > 0 else { return "Start" }
        return "Move \(frame.ply) of \(lastPly)"
    }

    var resultText: String? {
        guard let session, session.isFinished else { return nil }
        let headline = SessionStatusText.headline(session, me: "")
        return SessionStatusText.detail(session).map { "\(headline) \($0)" } ?? headline
    }

    var moves: [String] {
        guard let session else { return [] }
        switch session.kind {
        case .chess: return session.chess.san
        case .quoridor: return session.quoridor.moves
        case .ticTacToe: return []
        }
    }

    var highlightedPly: Int? {
        guard let frame, frame.ply > 0 else { return nil }
        return Int(frame.ply) - 1
    }

    func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let history = try await game.history(id: sessionID)
            session = history.session
            frames = history.frames.sorted { $0.ply < $1.ply }
            index = lastIndex
            error = nil
        } catch {
            self.error = switch error.code {
            case .notFound: "This game has no replay."
            case .unavailable: "The server is unreachable."
            default: error.message.isEmpty ? "Could not load the replay." : error.message
            }
        }
    }

    func first() { index = 0 }
    func previous() { index -= 1 }
    func next() { index += 1 }
    func last() { index = lastIndex }
}
