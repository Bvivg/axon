import Foundation
import Observation

@MainActor
@Observable
final class GameViewModel {
    typealias Sleeper = @Sendable (Duration) async throws -> Void

    let sessionID: String
    let me: String
    private(set) var session: Session?
    private(set) var connection: ChannelState = .connecting(attempt: 0)
    private(set) var synchronized = false
    private(set) var pending = false
    private(set) var error: String?
    private(set) var loadError: String?
    private(set) var names: [String: String] = [:]
    private(set) var receivedAt = Date.now
    private(set) var left = false
    @ObservationIgnored private let services: Services
    @ObservationIgnored private let sleep: Sleeper
    @ObservationIgnored private let pendingTimeout: Duration
    @ObservationIgnored private var channel: (any GameChannel)?
    @ObservationIgnored private var listener: Task<Void, Never>?
    @ObservationIgnored private var watchdog: Task<Void, Never>?
    @ObservationIgnored private var pendingRevision: UInt32?
    @ObservationIgnored private var requestedNames: Set<String> = []

    init(
        sessionID: String,
        me: String,
        services: Services,
        pendingTimeout: Duration = .seconds(5),
        sleep: @escaping Sleeper = { try await Task.sleep(for: $0) }
    ) {
        self.sessionID = sessionID
        self.me = me
        self.services = services
        self.pendingTimeout = pendingTimeout
        self.sleep = sleep
    }

    var kind: GameKind { session?.kind ?? .ticTacToe }
    var mySeat: Int? { session?.seat(of: me) }
    var isSpectator: Bool { session.map { $0.isSpectator(me) } ?? false }
    var perspective: Int { mySeat ?? 0 }
    var connected: Bool { connection == .open && synchronized }
    var isPlaying: Bool { session?.status == .playing }
    var isFinished: Bool { session?.isFinished ?? false }
    var isMyTurn: Bool { isPlaying && session?.turnUserID == me }
    var interactive: Bool { isMyTurn && connected && !pending }

    var statusText: String {
        guard let session else { return loadError == nil ? "Loading" : "Unavailable" }
        return SessionStatusText.headline(session, me: me)
    }

    var detailText: String? {
        guard let session else { return nil }
        return SessionStatusText.detail(session) ?? SessionStatusText.hint(session, me: me)
    }

    var canJoin: Bool {
        guard let session else { return false }
        return session.status == .waiting && !session.playerIds.contains(me)
    }

    var canResign: Bool {
        guard let session, mySeat != nil else { return false }
        return session.status == .playing || session.status == .waiting
    }

    var canOfferDraw: Bool {
        guard let session, mySeat != nil, kind.supportsDrawOffers else { return false }
        return session.status == .playing && !session.hasDrawOfferUserID
    }

    var drawOfferedToMe: Bool {
        guard let session, mySeat != nil, session.status == .playing, session.hasDrawOfferUserID else { return false }
        return session.drawOfferUserID != me
    }

    var drawOfferedByMe: Bool {
        guard let session, session.status == .playing, session.hasDrawOfferUserID else { return false }
        return session.drawOfferUserID == me
    }

    var canUndo: Bool {
        guard let session, mySeat != nil, kind == .ticTacToe else { return false }
        let rated = session.hasRated && session.rated
        return session.status == .playing && session.ply > 0 && !rated && connected && !pending
    }

    var resignPrompt: String {
        session?.status == .waiting ? "Cancel this game?" : "Resign this game?"
    }

    var moves: [String] {
        guard let session else { return [] }
        switch session.kind {
        case .chess: return session.chess.san
        case .quoridor: return session.quoridor.moves
        case .checkers: return session.checkers.moves
        case .ticTacToe: return []
        }
    }

    var spectators: Int {
        guard let session, session.hasSpectators else { return 0 }
        return Int(session.spectators)
    }

    func name(seat: Int) -> String {
        guard let session, seat < session.playerIds.count else { return "Waiting…" }
        let id = session.playerIds[seat]
        if id == me { return "You" }
        return names[id] ?? "Player"
    }

    func remaining(seat: Int) -> Duration? {
        guard let ms = session?.remainingMilliseconds(seat: seat) else { return nil }
        return .milliseconds(Int64(ms))
    }

    func clockRunning(seat: Int) -> Bool {
        guard let session, session.status == .playing, session.hasTurnUserID else { return false }
        return session.playerIds.firstIndex(of: session.turnUserID) == seat
    }

    func start() async {
        do {
            let loaded = try await services.game.session(id: sessionID)
            guard loaded.isSupported else {
                loadError = Self.unsupportedGame
                return
            }
            apply(loaded)
            loadError = nil
        } catch {
            loadError = Self.message(for: error)
        }
        guard loadError == nil || session != nil else { return }
        connect()
        await loadNames()
    }

    func stop() {
        listener?.cancel()
        listener = nil
        watchdog?.cancel()
        watchdog = nil
        if let channel {
            Task { await channel.close() }
        }
        channel = nil
    }

    func retry() async {
        error = nil
        if session == nil {
            await start()
        } else if connection == .closed {
            stop()
            connect()
        } else {
            await channel?.resync()
        }
    }

    func join() async {
        do {
            apply(try await services.game.joinSession(id: sessionID))
            error = nil
            await channel?.resync()
            await loadNames()
        } catch {
            self.error = Self.message(for: error)
        }
    }

    func resign() async {
        let waiting = session?.status == .waiting
        do {
            let result = try await services.game.leaveSession(id: sessionID)
            if let result { apply(result) }
            if waiting { left = true }
        } catch {
            self.error = Self.message(for: error)
        }
    }

    func handle(_ event: ChannelEvent) {
        switch event {
        case .state(let state):
            connection = state
            if state != .open {
                synchronized = false
                clearPending()
            }
        case .session(let next):
            guard next.id == sessionID else { return }
            apply(next)
            synchronized = true
            connection = .open
        case .error(let failure):
            clearPending()
            error = Self.message(for: failure)
        case .match:
            break
        }
    }

    private func connect() {
        let channel = services.channels.open(.session(sessionID))
        self.channel = channel
        listener = Task { [weak self] in
            for await event in channel.events {
                guard let self else { return }
                self.handle(event)
            }
        }
    }

    fileprivate func send(_ command: SessionCommand) async {
        guard connected, pendingRevision == nil, mySeat != nil, let session, let channel else { return }
        pendingRevision = session.revisionValue
        pending = true
        error = nil
        await channel.send(command)
        armWatchdog()
    }

    private func armWatchdog() {
        watchdog?.cancel()
        let timeout = pendingTimeout
        let sleep = sleep
        watchdog = Task { [weak self] in
            do {
                try await sleep(timeout)
            } catch {
                return
            }
            guard let self, self.pending else { return }
            self.clearPending()
            self.synchronized = false
            await self.channel?.resync()
        }
    }

    private func clearPending() {
        pendingRevision = nil
        pending = false
        watchdog?.cancel()
        watchdog = nil
    }

    private func apply(_ next: Session) {
        if let current = session, current.revisionValue > next.revisionValue { return }
        session = next
        receivedAt = .now
        if let pendingRevision, next.revisionValue > pendingRevision {
            clearPending()
        }
        if let id = next.playerIds.first(where: { $0 != me && names[$0] == nil && !requestedNames.contains($0) }) {
            requestedNames.insert(id)
            Task { await self.loadName(id) }
        }
    }

    private func loadNames() async {
        guard let session else { return }
        let ids = session.playerIds.filter { $0 != me && names[$0] == nil && !requestedNames.contains($0) }
        requestedNames.formUnion(ids)
        guard !ids.isEmpty, let profiles = try? await services.auth.profiles(ids: ids) else { return }
        for (id, profile) in profiles {
            names[id] = LobbyViewModel.displayName(profile)
        }
    }

    private func loadName(_ id: String) async {
        guard let profiles = try? await services.auth.profiles(ids: [id]), let profile = profiles[id] else {
            requestedNames.remove(id)
            return
        }
        names[id] = LobbyViewModel.displayName(profile)
    }
}

extension GameViewModel {
    func tap(cell: Int) async {
        guard let session, isMyTurn else { return }
        var command = SessionCommand()
        command.move = Axon_Game_V1_TicTacToeMove.with {
            $0.ply = session.ply
            $0.cell = UInt32(cell)
            if session.hasRevision { $0.revision = session.revision }
        }
        await send(command)
    }

    func chessMove(_ uci: String) async {
        guard let session, isMyTurn else { return }
        var command = SessionCommand()
        command.chessMove = Axon_Game_V1_ChessMove.with {
            $0.ply = session.ply
            $0.uci = uci
            if session.hasRevision { $0.revision = session.revision }
        }
        await send(command)
    }

    func quoridorMove(_ move: String) async {
        guard let session, isMyTurn else { return }
        var command = SessionCommand()
        command.quoridorMove = Axon_Game_V1_QuoridorMove.with {
            $0.ply = session.ply
            $0.move = move
            if session.hasRevision { $0.revision = session.revision }
        }
        await send(command)
    }

    func checkersMove(_ move: String) async {
        guard let session, isMyTurn else { return }
        var command = SessionCommand()
        command.checkersMove = Axon_Game_V1_CheckersMove.with {
            $0.ply = session.ply
            $0.move = move
            if session.hasRevision { $0.revision = session.revision }
        }
        await send(command)
    }

    func draw(_ action: DrawAction) async {
        var command = SessionCommand()
        command.draw = Axon_Game_V1_DrawCommand.with { $0.action = action }
        await send(command)
    }

    func play(_ move: String) async {
        switch kind {
        case .chess: await chessMove(move)
        case .quoridor: await quoridorMove(move)
        case .checkers: await checkersMove(move)
        case .ticTacToe: break
        }
    }

    func undo() async {
        guard let session, canUndo else { return }
        var command = SessionCommand()
        command.undo = Axon_Game_V1_UndoMove.with {
            if session.hasRevision { $0.revision = session.revision }
        }
        await send(command)
    }
}

extension GameViewModel {
    static let unsupportedGame = "This game is not available in the app yet."

    static func message(for error: SessionError) -> String {
        sessionErrorMessages[error] ?? "Could not play that move."
    }

    static func message(for error: APIError) -> String {
        switch error.code {
        case .notFound: "This game does not exist or is private."
        case .failedPrecondition: error.message.isEmpty ? "That is not possible right now." : error.message
        case .unavailable: "The server is unreachable."
        case .unauthenticated: "Your session expired. Sign in again."
        default: error.message.isEmpty ? "Something went wrong." : error.message
        }
    }
}

private let sessionErrorMessages: [SessionError: String] = [
    .invalidCommand: "That move is invalid.",
    .notFound: "This game is unavailable.",
    .notYourTurn: "Wait for your turn.",
    .staleMove: "The board changed. Try again.",
    .cellTaken: "That square is already taken.",
    .notPlaying: "This game is not in progress.",
    .unavailable: "The game service is unavailable.",
    .undoDisabled: "Undo is off in rated games.",
    .illegalMove: "That move is not allowed.",
    .noDrawOffer: "The draw offer is no longer open.",
    .rateLimited: "You are moving too fast. Try again.",
    .spectator: "Spectators cannot move.",
]
