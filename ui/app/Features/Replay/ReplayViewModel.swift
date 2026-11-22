import Foundation
import Observation
import SwiftUI

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
    private(set) var report: GameAnalysis?
    private(set) var analysisFailed = false
    @ObservationIgnored private let game: any GameAPI
    @ObservationIgnored private let analysis: (any AnalysisAPI)?
    @ObservationIgnored let analysisInterval: Duration

    init(sessionID: String, game: any GameAPI, analysis: (any AnalysisAPI)? = nil, analysisInterval: Duration = .seconds(3)) {
        self.sessionID = sessionID
        self.game = game
        self.analysis = analysis
        self.analysisInterval = analysisInterval
    }

    var analysed: Bool {
        guard let session, analysis != nil else { return false }
        return session.kind == .chess && session.rated && session.isFinished && !session.chess.san.isEmpty
    }

    var analysisReady: GameAnalysis? {
        report?.status == .ready ? report : nil
    }

    var analysisState: AnalysisState {
        if analysisFailed { return .error }
        switch report?.status {
        case .ready: return .ready
        case .failed: return .failed
        default: return .pending
        }
    }

    var currentAnalysis: PositionAnalysis? {
        guard let frame, let ready = analysisReady else { return nil }
        return ready.positions.first { $0.ply == frame.ply }
    }

    var verdict: AnalysisVerdict? {
        guard let frame, let ready = analysisReady, let position = currentAnalysis else { return nil }
        guard frame.ply > 0 else { return AnalysisVerdict(evaluation: position.evaluationLabel, move: nil, best: nil) }
        let san = session?.chess.san ?? []
        let played = san.indices.contains(Int(frame.ply) - 1) ? san[Int(frame.ply) - 1] : ""
        let previous = ready.positions.first { $0.ply == frame.ply - 1 }
        let before = frames.first { $0.ply == frame.ply - 1 }?.chess.fen ?? ""
        let best = position.moveClass == .best ? nil : previous.flatMap { prior in
            prior.hasBestMove ? AnalysisNotation.move(prior.bestMove, in: before) : nil
        }
        let marked = played + (position.moveClass.mark?.rawValue ?? "")
        let title = position.moveClass.title
        return AnalysisVerdict(
            evaluation: position.evaluationLabel,
            move: title.isEmpty ? marked : "\(marked) · \(title)",
            best: best.map { "Best \($0)" }
        )
    }

    func watchAnalysis() async {
        guard analysed, let analysis else { return }
        while !Task.isCancelled {
            do {
                report = try await analysis.analysis(of: sessionID)
                analysisFailed = false
            } catch {
                analysisFailed = true
                return
            }
            guard report == nil || report?.status == .pending else { return }
            try? await Task.sleep(for: analysisInterval)
        }
    }

    func retryAnalysis() async {
        analysisFailed = false
        await watchAnalysis()
    }

    func select(ply: Int) {
        if let target = frames.firstIndex(where: { Int($0.ply) == ply }) {
            index = target
        }
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
        case .chess:
            guard let ready = analysisReady else { return session.chess.san }
            return session.chess.san.enumerated().map { offset, san in
                let mark = ready.positions.first { Int($0.ply) == offset + 1 }?.moveClass.mark
                return san + (mark?.rawValue ?? "")
            }
        case .quoridor: return session.quoridor.moves
        case .checkers: return session.checkers.moves
        case .durak: return session.durak.moves.map(DurakNotation.label)
        case .ticTacToe: return []
        }
    }

    var highlightedPly: Int? {
        guard let frame, frame.ply > 0 else { return nil }
        return Int(frame.ply)
    }

    func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            let history = try await game.history(id: sessionID)
            guard history.session.isSupported else {
                error = GameViewModel.unsupportedGame
                return
            }
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

    var sliderValue: Binding<Double> {
        Binding(get: { Double(self.index) }, set: { self.index = Int($0.rounded()) })
    }

    func first() { index = 0 }
    func previous() { index -= 1 }
    func next() { index += 1 }
    func last() { index = lastIndex }
}

enum AnalysisState: Equatable, Sendable {
    case pending
    case ready
    case failed
    case error
}

struct AnalysisVerdict: Equatable, Sendable {
    let evaluation: String
    let move: String?
    let best: String?
}
