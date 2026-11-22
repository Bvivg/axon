import Foundation

struct LiveTournamentsAPI: TournamentsAPI {
    let client: any Axon_Tournament_V1_TournamentServiceClientInterface
    let connect: ConnectClient

    func list() async throws(APIError) -> [Tournament] {
        var request = Axon_Tournament_V1_ListTournamentsRequest()
        request.limit = 50
        return try await connect.call { headers in
            await client.listTournaments(request: request, headers: headers)
        }.tournaments
    }

    func tournament(_ id: String) async throws(APIError) -> Tournament {
        var request = Axon_Tournament_V1_GetTournamentRequest()
        request.tournamentID = id
        return try await connect.call { headers in
            await client.getTournament(request: request, headers: headers)
        }.tournament
    }

    func create(_ spec: NewTournament, request id: String) async throws(APIError) -> Tournament {
        var request = Axon_Tournament_V1_CreateTournamentRequest()
        request.requestID = id
        request.name = spec.name
        request.game = spec.game.rawValue
        request.format = spec.format
        request.capacity = UInt32(spec.capacity)
        if let rounds = spec.rounds {
            request.rounds = UInt32(rounds)
        }
        return try await connect.call { headers in
            await client.createTournament(request: request, headers: headers)
        }.tournament
    }

    func perform(_ action: TournamentAction, on id: String, reason: String?) async throws(APIError) -> Tournament {
        switch action {
        case .register:
            var request = Axon_Tournament_V1_RegisterForTournamentRequest()
            request.tournamentID = id
            return try await connect.call { headers in
                await client.registerForTournament(request: request, headers: headers)
            }.tournament
        case .withdraw:
            var request = Axon_Tournament_V1_WithdrawFromTournamentRequest()
            request.tournamentID = id
            return try await connect.call { headers in
                await client.withdrawFromTournament(request: request, headers: headers)
            }.tournament
        case .start:
            var request = Axon_Tournament_V1_StartTournamentRequest()
            request.tournamentID = id
            if let reason { request.reason = reason }
            return try await connect.call { headers in
                await client.startTournament(request: request, headers: headers)
            }.tournament
        case .cancel:
            var request = Axon_Tournament_V1_CancelTournamentRequest()
            request.tournamentID = id
            if let reason { request.reason = reason }
            return try await connect.call { headers in
                await client.cancelTournament(request: request, headers: headers)
            }.tournament
        }
    }
}

struct LiveAnalysisAPI: AnalysisAPI {
    let client: any Axon_Analysis_V1_AnalysisServiceClientInterface
    let connect: ConnectClient

    func analysis(of session: String) async throws(APIError) -> GameAnalysis? {
        var request = Axon_Analysis_V1_GetAnalysisRequest()
        request.sessionID = session
        do {
            return try await connect.call { headers in
                await client.getAnalysis(request: request, headers: headers)
            }
        } catch where error.code == .notFound {
            return nil
        }
    }
}
