import Foundation
@testable import Axon

final class FakeTournaments: TournamentsAPI, @unchecked Sendable {
    struct Performed {
        let action: TournamentAction
        let id: String
        let reason: String?
    }

    var listed: [Tournament] = []
    var stored: [String: Tournament] = [:]
    var failure: APIError?
    var created: [(NewTournament, String)] = []
    var actions: [Performed] = []
    var actionResult: Result<Tournament, APIError>?
    var loads = 0

    func list() async throws(APIError) -> [Tournament] {
        if let failure { throw failure }
        return listed
    }

    func tournament(_ id: String) async throws(APIError) -> Tournament {
        loads += 1
        if let failure { throw failure }
        guard let tournament = stored[id] else { throw APIError(.notFound) }
        return tournament
    }

    func create(_ spec: NewTournament, request: String) async throws(APIError) -> Tournament {
        created.append((spec, request))
        if let failure { throw failure }
        let tournament = Tournament.with {
            $0.id = "t-new"
            $0.name = spec.name
            $0.game = spec.game.rawValue
            $0.format = spec.format
            $0.capacity = UInt32(spec.capacity)
            $0.status = .registration
        }
        stored[tournament.id] = tournament
        return tournament
    }

    func perform(_ action: TournamentAction, on id: String, reason: String?) async throws(APIError) -> Tournament {
        actions.append(Performed(action: action, id: id, reason: reason))
        if let actionResult { return try actionResult.get() }
        guard let tournament = stored[id] else { throw APIError(.notFound) }
        return tournament
    }
}

final class FakeAnalysis: AnalysisAPI, @unchecked Sendable {
    var reports: [GameAnalysis?] = []
    var failure: APIError?
    var requests = 0

    func analysis(of session: String) async throws(APIError) -> GameAnalysis? {
        requests += 1
        if let failure { throw failure }
        guard !reports.isEmpty else { return nil }
        return reports.count == 1 ? reports[0] : reports.removeFirst()
    }
}
