import Foundation
import Observation

@MainActor
@Observable
final class LobbyViewModel {
    private(set) var mine: [Session] = []
    private(set) var live: [Session] = []
    private(set) var names: [String: String] = [:]
    private(set) var ratings: [GameKind: Rating] = [:]
    private(set) var isLoading = false
    private(set) var busy: String?
    private(set) var error: String?
    private(set) var matching: GameKind?
    private(set) var ticket: MatchTicket?
    var joinID = ""
    let me: String
    @ObservationIgnored private let services: Services
    @ObservationIgnored private let open: (Route) -> Void
    @ObservationIgnored private var matchChannel: (any GameChannel)?
    @ObservationIgnored private var matchTask: Task<Void, Never>?
    @ObservationIgnored let refreshInterval: Duration
    @ObservationIgnored private var ratedAfter: Int?

    init(services: Services, me: String, refreshInterval: Duration = .seconds(15), open: @escaping (Route) -> Void) {
        self.services = services
        self.me = me
        self.refreshInterval = refreshInterval
        self.open = open
    }

    var active: [Session] { mine.filter { !$0.isFinished } }
    var finished: [Session] { mine.filter(\.isFinished) }

    func rating(_ game: GameKind) -> UInt32 {
        ratings[game]?.rating ?? Rating.starting
    }

    func mine(_ game: GameKind) -> [Session] {
        mine.filter { $0.kind == game }
    }

    func live(_ game: GameKind) -> [Session] {
        live.filter { $0.kind == game }
    }

    func name(_ id: String) -> String {
        names[id] ?? "Player"
    }

    func opponentName(in session: Session) -> String {
        guard let opponent = session.playerIds.first(where: { $0 != me }) else { return "No opponent yet" }
        return name(opponent)
    }

    func run() async {
        while !Task.isCancelled {
            await refresh()
            try? await Task.sleep(for: refreshInterval)
        }
    }

    func refresh() async {
        isLoading = true
        defer { isLoading = false }
        do {
            async let ownTask = services.game.mySessions()
            async let liveTask = services.game.liveSessions(nil)
            let (own, current) = try await (ownTask, liveTask)
            mine = own.filter(\.isSupported).sorted(by: Self.newestFirst)
            live = current.filter { $0.isSupported && !$0.playerIds.contains(me) }.sorted(by: Self.newestFirst)
            error = nil
        } catch let failure as APIError {
            error = Self.message(failure)
        } catch {
            self.error = "Could not load games."
        }
        await loadNames()
        await loadRatings()
    }

    private func loadRatings() async {
        let finishedCount = finished.count
        guard ratedAfter != finishedCount else { return }
        ratedAfter = finishedCount
        await withTaskGroup(of: (GameKind, Rating?).self) { [services, me] group in
            for game in GameKind.allCases {
                group.addTask { (game, try? await services.game.rating(game, user: me)) }
            }
            for await (game, rating) in group {
                if let rating {
                    ratings[game] = rating
                } else {
                    ratedAfter = nil
                }
            }
        }
    }

    func create(_ game: GameKind) async {
        await perform("create.\(game.rawValue)") { [services] () async throws(APIError) -> Session in
            try await services.game.createSession(game)
        }
    }

    func join() async {
        guard let id = Self.sessionID(in: joinID) else {
            error = "Paste a game ID or link."
            return
        }
        await perform("join") { [services] () async throws(APIError) -> Session in
            try await services.game.joinSession(id: id)
        }
        joinID = ""
    }

    func quickMatch(_ game: GameKind) async {
        guard matching == nil else { return }
        error = nil
        busy = "match.\(game.rawValue)"
        defer { busy = nil }
        do {
            let ticket = try await services.game.findMatch(game)
            if handle(ticket) { return }
            matching = game
            self.ticket = ticket
            listen(game)
        } catch {
            self.error = Self.message(error)
        }
    }

    func cancelMatch() async {
        guard let game = matching else { return }
        stopListening()
        matching = nil
        ticket = nil
        do {
            try await services.game.cancelMatch(game)
        } catch {
            self.error = Self.message(error)
        }
    }

    func stop() {
        stopListening()
    }

    func openGame(_ id: String) {
        open(.game(id))
    }

    func openReplay(_ id: String) {
        open(.replay(id))
    }

    func openPractice(_ game: GameKind) {
        open(.practice(game))
    }

    private func listen(_ game: GameKind) {
        let channel = services.channels.open(.match(game))
        matchChannel = channel
        matchTask = Task { [weak self] in
            for await event in channel.events {
                guard let self else { return }
                switch event {
                case .match(let ticket):
                    self.ticket = ticket
                    if self.handle(ticket) { return }
                case .state(.closed):
                    if self.matching != nil {
                        self.error = "Matchmaking connection lost."
                        self.matching = nil
                    }
                    return
                default:
                    break
                }
            }
        }
    }

    private func stopListening() {
        matchTask?.cancel()
        matchTask = nil
        if let channel = matchChannel {
            Task { await channel.close() }
        }
        matchChannel = nil
    }

    private func handle(_ ticket: MatchTicket) -> Bool {
        guard ticket.status == .matched, ticket.hasSessionID, !ticket.sessionID.isEmpty else { return false }
        stopListening()
        matching = nil
        self.ticket = nil
        open(.game(ticket.sessionID))
        return true
    }

    private func perform(_ action: String, _ operation: @escaping () async throws(APIError) -> Session) async {
        guard busy == nil else { return }
        busy = action
        error = nil
        defer { busy = nil }
        do {
            let session = try await operation()
            open(.game(session.id))
        } catch {
            self.error = Self.message(error)
        }
    }

    private func loadNames() async {
        let ids = Set((mine + live).flatMap(\.playerIds)).subtracting(names.keys)
        guard !ids.isEmpty else { return }
        guard let profiles = try? await services.auth.profiles(ids: Array(ids)) else { return }
        for (id, profile) in profiles {
            names[id] = Self.displayName(profile)
        }
    }

    nonisolated static func displayName(_ profile: PublicProfile) -> String {
        profile.shownName
    }

    static func sessionID(in text: String) -> String? {
        let pattern = /[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}/
        return text.matches(of: pattern).last.map { String($0.output).lowercased() }
    }

    static func message(_ error: APIError) -> String {
        switch error.code {
        case .notFound: "That game does not exist."
        case .failedPrecondition: error.message.isEmpty ? "That game cannot be joined." : error.message
        case .resourceExhausted: "Slow down and try again in a moment."
        case .unavailable: "The server is unreachable."
        case .unauthenticated: "Your session expired. Sign in again."
        default: error.message.isEmpty ? "Something went wrong." : error.message
        }
    }

    private static func newestFirst(_ lhs: Session, _ rhs: Session) -> Bool {
        if lhs.updatedAt.seconds != rhs.updatedAt.seconds { return lhs.updatedAt.seconds > rhs.updatedAt.seconds }
        return lhs.id > rhs.id
    }
}
