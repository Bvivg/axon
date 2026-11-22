import Foundation

struct LiveGameAPI: GameAPI {
    let client: any Axon_Game_V1_GameServiceClientInterface
    let connect: ConnectClient

    static func requestID() -> String {
        UUID().uuidString.lowercased()
    }

    func createSession(_ game: GameKind) async throws(APIError) -> Session {
        var request = Axon_Game_V1_CreateSessionRequest()
        request.game = game.rawValue
        request.requestID = Self.requestID()
        return try await connect.call { headers in
            await client.createSession(request: request, headers: headers)
        }.session
    }

    func joinSession(id: String) async throws(APIError) -> Session {
        var request = Axon_Game_V1_JoinSessionRequest()
        request.sessionID = id
        return try await connect.call { headers in
            await client.joinSession(request: request, headers: headers)
        }.session
    }

    func session(id: String) async throws(APIError) -> Session {
        var request = Axon_Game_V1_GetSessionRequest()
        request.sessionID = id
        return try await connect.call { headers in
            await client.getSession(request: request, headers: headers)
        }.session
    }

    func mySessions() async throws(APIError) -> [Session] {
        try await connect.call { headers in
            await client.listSessions(request: Axon_Game_V1_ListSessionsRequest(), headers: headers)
        }.sessions
    }

    func liveSessions(_ game: GameKind?) async throws(APIError) -> [Session] {
        var request = Axon_Game_V1_ListLiveSessionsRequest()
        if let game {
            request.game = game.rawValue
        }
        return try await connect.call { headers in
            await client.listLiveSessions(request: request, headers: headers)
        }.sessions
    }

    func findMatch(_ game: GameKind) async throws(APIError) -> MatchTicket {
        var request = Axon_Game_V1_FindMatchRequest()
        request.game = game.rawValue
        request.requestID = Self.requestID()
        return try await connect.call { headers in
            await client.findMatch(request: request, headers: headers)
        }.ticket
    }

    func match(_ game: GameKind) async throws(APIError) -> MatchTicket {
        var request = Axon_Game_V1_GetMatchRequest()
        request.game = game.rawValue
        return try await connect.call { headers in
            await client.getMatch(request: request, headers: headers)
        }.ticket
    }

    func cancelMatch(_ game: GameKind) async throws(APIError) {
        var request = Axon_Game_V1_CancelMatchRequest()
        request.game = game.rawValue
        _ = try await connect.call { headers in
            await client.cancelMatch(request: request, headers: headers)
        }
    }

    func leaveSession(id: String) async throws(APIError) -> Session? {
        var request = Axon_Game_V1_LeaveSessionRequest()
        request.sessionID = id
        let response = try await connect.call { headers in
            await client.leaveSession(request: request, headers: headers)
        }
        return response.hasSession ? response.session : nil
    }

    func history(id: String) async throws(APIError) -> SessionHistory {
        var request = Axon_Game_V1_GetSessionHistoryRequest()
        request.sessionID = id
        return try await connect.call { headers in
            await client.getSessionHistory(request: request, headers: headers)
        }
    }

    func rating(_ game: GameKind, user: String) async throws(APIError) -> Rating {
        var request = Axon_Game_V1_GetRatingsRequest()
        request.game = game.rawValue
        request.userIds = [user]
        let ratings = try await connect.call { headers in
            await client.getRatings(request: request, headers: headers)
        }.ratings
        return ratings.first { $0.userID == user } ?? Rating.with {
            $0.userID = user
            $0.rating = Rating.starting
        }
    }
}
