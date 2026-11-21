import Connect
import Foundation
import Testing
@testable import Axon

@Suite(.timeLimit(.minutes(1)))
struct LiveGameAPITests {
    let client = ScriptedGameClient()

    var api: LiveGameAPI {
        LiveGameAPI(client: client, connect: ConnectClient(vault: Fixtures.vault(Fixtures.tokens("a-1"))))
    }

    func serve<Output: ProtobufMessage & Sendable>(_ method: String, _ response: Output) {
        client.script.on(method) { (_: String?) -> Result<Output, ConnectError> in .success(response) }
    }

    func request<Input>(_ method: String, as type: Input.Type) -> [Input] {
        client.script.recorded(method).compactMap { $0.request as? Input }
    }

    @Test
    func createSessionSendsAFreshRequestID() async throws {
        var response = Axon_Game_V1_CreateSessionResponse()
        response.session = Fixtures.session("s-1")
        serve("CreateSession", response)
        let api = api
        #expect(try await api.createSession(.chess).id == "s-1")
        _ = try await api.createSession(.chess)
        let requests = request("CreateSession", as: Axon_Game_V1_CreateSessionRequest.self)
        #expect(requests.map(\.game) == ["chess", "chess"])
        #expect(requests.allSatisfy { UUID(uuidString: $0.requestID) != nil })
        #expect(Set(requests.map(\.requestID)).count == 2)
        #expect(client.script.recorded.allSatisfy { $0.authorization == "Bearer a-1" })
    }

    @Test
    func findMatchSendsAFreshRequestID() async throws {
        var response = Axon_Game_V1_FindMatchResponse()
        response.ticket.game = "quoridor"
        response.ticket.status = .searching
        serve("FindMatch", response)
        let api = api
        #expect(try await api.findMatch(.quoridor).status == .searching)
        _ = try await api.findMatch(.quoridor)
        let requests = request("FindMatch", as: Axon_Game_V1_FindMatchRequest.self)
        #expect(requests.map(\.game) == ["quoridor", "quoridor"])
        #expect(requests.allSatisfy { UUID(uuidString: $0.requestID) != nil })
        #expect(Set(requests.map(\.requestID)).count == 2)
    }

    @Test
    func retriedCreateKeepsItsRequestID() async throws {
        typealias Created = Result<Axon_Game_V1_CreateSessionResponse, ConnectError>
        client.script.on("CreateSession") { (authorization: String?) -> Created in
            guard authorization == "Bearer a-2" else { return .failure(Fixtures.denied(.unauthenticated)) }
            var response = Axon_Game_V1_CreateSessionResponse()
            response.session = Fixtures.session("s-1")
            return .success(response)
        }
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2")))
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        _ = try await LiveGameAPI(client: client, connect: ConnectClient(vault: vault)).createSession(.ticTacToe)
        let requests = request("CreateSession", as: Axon_Game_V1_CreateSessionRequest.self)
        #expect(requests.count == 2)
        #expect(requests[0].requestID == requests[1].requestID)
    }

    @Test
    func sessionLookupsUseTheSessionID() async throws {
        var joined = Axon_Game_V1_JoinSessionResponse()
        joined.session = Fixtures.session("s-2")
        serve("JoinSession", joined)
        var fetched = Axon_Game_V1_GetSessionResponse()
        fetched.session = Fixtures.session("s-3")
        serve("GetSession", fetched)
        var history = SessionHistory()
        history.session = Fixtures.session("s-4")
        var frame = HistoryFrame()
        frame.ply = 1
        history.frames = [frame]
        serve("GetSessionHistory", history)

        #expect(try await api.joinSession(id: "s-2").id == "s-2")
        #expect(try await api.session(id: "s-3").id == "s-3")
        #expect(try await api.history(id: "s-4") == history)
        #expect(request("JoinSession", as: Axon_Game_V1_JoinSessionRequest.self).map(\.sessionID) == ["s-2"])
        #expect(request("GetSession", as: Axon_Game_V1_GetSessionRequest.self).map(\.sessionID) == ["s-3"])
        let replayed = request("GetSessionHistory", as: Axon_Game_V1_GetSessionHistoryRequest.self)
        #expect(replayed.map(\.sessionID) == ["s-4"])
    }

    @Test
    func listsSessions() async throws {
        var mine = Axon_Game_V1_ListSessionsResponse()
        mine.sessions = [Fixtures.session("s-1"), Fixtures.session("s-2")]
        serve("ListSessions", mine)
        var live = Axon_Game_V1_ListLiveSessionsResponse()
        live.sessions = [Fixtures.session("s-9")]
        serve("ListLiveSessions", live)

        #expect(try await api.mySessions().map(\.id) == ["s-1", "s-2"])
        #expect(try await api.liveSessions(nil).map(\.id) == ["s-9"])
        _ = try await api.liveSessions(.chess)
        let requests = request("ListLiveSessions", as: Axon_Game_V1_ListLiveSessionsRequest.self)
        #expect(requests.map(\.hasGame) == [false, true])
        #expect(requests.last?.game == "chess")
    }

    @Test
    func matchTicketsUseTheGameKey() async throws {
        var current = Axon_Game_V1_GetMatchResponse()
        current.ticket.game = "tictactoe"
        current.ticket.status = .idle
        serve("GetMatch", current)
        var cancelled = Axon_Game_V1_CancelMatchResponse()
        cancelled.ticket.status = .idle
        serve("CancelMatch", cancelled)

        #expect(try await api.match(.ticTacToe).status == .idle)
        try await api.cancelMatch(.ticTacToe)
        #expect(request("GetMatch", as: Axon_Game_V1_GetMatchRequest.self).map(\.game) == ["tictactoe"])
        #expect(request("CancelMatch", as: Axon_Game_V1_CancelMatchRequest.self).map(\.game) == ["tictactoe"])
    }

    @Test
    func leaveReturnsTheSessionOnlyWhenSet() async throws {
        var resigned = Axon_Game_V1_LeaveSessionResponse()
        resigned.session = Fixtures.session("s-1")
        serve("LeaveSession", resigned)
        #expect(try await api.leaveSession(id: "s-1")?.id == "s-1")

        var cancelled = Axon_Game_V1_LeaveSessionResponse()
        cancelled.cancelled = true
        serve("LeaveSession", cancelled)
        #expect(try await api.leaveSession(id: "s-1") == nil)
        #expect(request("LeaveSession", as: Axon_Game_V1_LeaveSessionRequest.self).map(\.sessionID) == ["s-1", "s-1"])
    }
}
