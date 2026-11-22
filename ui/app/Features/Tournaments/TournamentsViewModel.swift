import Foundation
import Observation

@MainActor
@Observable
final class TournamentsViewModel {
    private(set) var tournaments: [Tournament] = []
    private(set) var loaded = false
    private(set) var error: String?
    private(set) var creating = false
    private(set) var createError: String?
    @ObservationIgnored private let api: any TournamentsAPI
    @ObservationIgnored private var requestID: String?
    @ObservationIgnored let refreshInterval: Duration

    init(api: any TournamentsAPI, refreshInterval: Duration = .seconds(10)) {
        self.api = api
        self.refreshInterval = refreshInterval
    }

    func run() async {
        while !Task.isCancelled {
            await refresh()
            try? await Task.sleep(for: refreshInterval)
        }
    }

    func refresh() async {
        do {
            tournaments = try await api.list().filter { GameKind(rawValue: $0.game) != nil }
            error = nil
        } catch {
            self.error = Self.message(error, fallback: "Could not load tournaments.")
        }
        loaded = true
    }

    func create(_ spec: NewTournament) async -> Tournament? {
        let name = spec.name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty, !creating else { return nil }
        creating = true
        createError = nil
        defer { creating = false }
        let request = requestID ?? UUID().uuidString.lowercased()
        requestID = request
        var spec = spec
        spec.name = name
        spec.rounds = spec.format == .swiss ? spec.rounds : nil
        do {
            let tournament = try await api.create(spec, request: request)
            requestID = nil
            tournaments.removeAll { $0.id == tournament.id }
            tournaments.insert(tournament, at: 0)
            return tournament
        } catch {
            createError = Self.message(error, fallback: "Could not create the tournament. Try again.")
            return nil
        }
    }

    func discardDraft() {
        requestID = nil
        createError = nil
    }

    static func message(_ error: APIError, fallback: String) -> String {
        switch error.code {
        case .unavailable: "The server is unreachable."
        case .permissionDenied: "You can’t do that in this tournament."
        case .failedPrecondition, .invalidArgument: error.message.isEmpty ? fallback : error.message.capitalizedSentence
        default: fallback
        }
    }
}

@MainActor
@Observable
final class TournamentViewModel {
    let id: String
    let me: String
    let admin: Bool
    private(set) var tournament: Tournament?
    private(set) var error: String?
    private(set) var actionError: String?
    private(set) var busy = false
    private(set) var names: [String: String] = [:]
    private(set) var readyGames: Set<String> = []
    var confirming: TournamentAction?
    @ObservationIgnored private let api: any TournamentsAPI
    @ObservationIgnored private let services: Services
    @ObservationIgnored let refreshInterval: Duration

    init(id: String, services: Services, user: User, refreshInterval: Duration = .seconds(2)) {
        self.id = id
        self.services = services
        api = services.tournaments
        me = user.id
        admin = user.roles.contains("admin")
        self.refreshInterval = refreshInterval
    }

    var isCreator: Bool { tournament?.creatorID == me }
    var isRegistered: Bool { tournament?.isRegistered(me) ?? false }
    private var isOpen: Bool { tournament?.status == .registration }
    private var isLive: Bool { tournament?.status.isLive ?? false }

    var canRegister: Bool {
        guard let tournament, isOpen, !isRegistered else { return false }
        return tournament.playerCount < tournament.capacity
    }

    var canWithdraw: Bool { isOpen && isRegistered }
    var canStart: Bool { isOpen && (isCreator || admin) }
    var startEnabled: Bool { (tournament?.playerCount ?? 0) >= 2 }
    var canCancel: Bool { (isOpen && (isCreator || admin)) || (isLive && admin) }

    func name(_ id: String?) -> String {
        guard let id, !id.isEmpty else { return "TBD" }
        return names[id] ?? "Player"
    }

    func run() async {
        while !Task.isCancelled {
            await refresh()
            if tournament?.status.isOver == true { return }
            try? await Task.sleep(for: refreshInterval)
        }
    }

    func refresh() async {
        do {
            adopt(try await api.tournament(id))
            error = nil
        } catch {
            self.error = error.code == .notFound
                ? "This tournament no longer exists."
                : TournamentsViewModel.message(error, fallback: "Could not load the tournament.")
        }
        await loadNames()
        await checkGames()
    }

    func request(_ action: TournamentAction) {
        if overrides(action) {
            confirming = action
        } else {
            Task { await perform(action) }
        }
    }

    func perform(_ action: TournamentAction, reason: String? = nil) async {
        guard !busy else { return }
        busy = true
        actionError = nil
        defer { busy = false }
        do {
            adopt(try await api.perform(action, on: id, reason: reason))
            await loadNames()
        } catch {
            actionError = TournamentsViewModel.message(error, fallback: "That did not work. Try again.")
        }
    }

    func overrides(_ action: TournamentAction) -> Bool {
        guard admin, action == .start || action == .cancel else { return false }
        return !isCreator || (action == .cancel && isLive)
    }

    func isReady(_ match: TournamentMatch) -> Bool {
        guard match.hasSessionID else { return false }
        return match.result != .pending || readyGames.contains(match.sessionID)
    }

    private func adopt(_ next: Tournament) {
        tournament = next
    }

    private func loadNames() async {
        guard let tournament else { return }
        let ids = Set(tournament.registrations.map(\.userID)).subtracting(names.keys)
        guard !ids.isEmpty, let profiles = try? await services.auth.profiles(ids: Array(ids)) else { return }
        for (id, profile) in profiles {
            names[id] = profile.shownName
        }
    }

    private func checkGames() async {
        guard let tournament else { return }
        let waiting = tournament.rounds
            .flatMap(\.matches)
            .filter { $0.result == .pending && $0.hasSessionID && !readyGames.contains($0.sessionID) }
            .map(\.sessionID)
        for session in waiting {
            guard (try? await services.game.session(id: session)) != nil else { continue }
            readyGames.insert(session)
        }
    }
}

private extension String {
    var capitalizedSentence: String {
        prefix(1).uppercased() + dropFirst()
    }
}
