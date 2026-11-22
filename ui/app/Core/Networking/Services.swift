import Foundation

struct APIError: Error, Equatable, Sendable {
    enum Code: Equatable, Sendable {
        case unauthenticated
        case invalidArgument
        case alreadyExists
        case notFound
        case permissionDenied
        case failedPrecondition
        case resourceExhausted
        case unavailable
        case cancelled
        case unknown
    }

    let code: Code
    let message: String

    init(_ code: Code, _ message: String = "") {
        self.code = code
        self.message = message
    }
}

protocol AuthAPI: Sendable {
    func restore() async -> User?
    func register(email: String, username: String, password: String) async throws(APIError) -> User
    func login(email: String, password: String) async throws(APIError) -> User
    func startOAuth(_ provider: OAuthProvider, challenge: String) async throws(APIError) -> OAuthStart
    func completeOAuth(_ provider: OAuthProvider, code: String, state: String, verifier: String) async throws(APIError) -> User
    func logout() async
    func profiles(ids: [String]) async throws(APIError) -> [String: PublicProfile]
    func account() async throws(APIError) -> Account
    func updateProfile(_ changes: ProfileChanges) async throws(APIError) -> User
    func sessions() async throws(APIError) -> [SignInSession]
    func revokeSession(id: String) async throws(APIError)
    func startLink(_ provider: OAuthProvider, challenge: String) async throws(APIError) -> OAuthStart
    func completeLink(_ provider: OAuthProvider, code: String, state: String, verifier: String) async throws(APIError) -> User
    func searchUsers(_ query: String) async throws(APIError) -> [PublicProfile]
    var signedOut: AsyncStream<Void> { get }
}

protocol GameAPI: Sendable {
    func createSession(_ game: GameKind) async throws(APIError) -> Session
    func joinSession(id: String) async throws(APIError) -> Session
    func session(id: String) async throws(APIError) -> Session
    func mySessions() async throws(APIError) -> [Session]
    func liveSessions(_ game: GameKind?) async throws(APIError) -> [Session]
    func findMatch(_ game: GameKind) async throws(APIError) -> MatchTicket
    func match(_ game: GameKind) async throws(APIError) -> MatchTicket
    func cancelMatch(_ game: GameKind) async throws(APIError)
    func leaveSession(id: String) async throws(APIError) -> Session?
    func history(id: String) async throws(APIError) -> SessionHistory
    func rating(_ game: GameKind, user: String) async throws(APIError) -> Rating
}

struct MessagePage: Equatable, Sendable {
    let messages: [ChatMessage]
    let hasMore: Bool
}

struct RoomDetails: Equatable, Sendable {
    let room: Room
    let members: [RoomMember]
}

struct HistoryQuery: Equatable, Sendable {
    var before: Int64?
    var after: Int64?
    var limit: Int32 = 50
    var kinds: [Axon_Chat_V1_MessageKind] = []
    var groupedItems = false
}

protocol ChatAPI: Sendable {
    func rooms() async throws(APIError) -> [Room]
    func room(id: String) async throws(APIError) -> RoomDetails
    func messages(in room: String, _ query: HistoryQuery) async throws(APIError) -> MessagePage
    func search(in room: String, for text: String) async throws(APIError) -> [ChatMessage]
    func markRead(room: String, seq: Int64) async throws(APIError)
    func directRoom(with user: String) async throws(APIError) -> Room?
    func createGroup(name: String, members: [String]) async throws(APIError) -> Room
    func addMembers(_ users: [String], to room: String) async throws(APIError)
    func removeMember(_ user: String, from room: String) async throws(APIError)
    func renameGroup(_ room: String, to name: String) async throws(APIError) -> Room
    func leave(room: String) async throws(APIError)
    func hide(room: String) async throws(APIError)
    func readers(of message: String) async throws(APIError) -> [MessageReader]
    func forward(_ messages: [String], to rooms: [String], comment: String, clientID: String) async throws(APIError) -> Int
}

protocol CallsAPI: Sendable {
    func history(before: String?, limit: Int) async throws(APIError) -> CallPage
    func register(resume: EndpointRef?) async throws(APIError) -> EndpointRef
    func capabilities() async throws(APIError) -> CallCapabilities
    func start(_ target: CallTarget, mode: CallMode, endpoint: EndpointRef, request: String) async throws(APIError) -> Call
    func accept(_ call: String, endpoint: EndpointRef, request: String) async throws(APIError) -> Call
    func terminate(_ call: String, _ action: CallTermination, endpoint: EndpointRef, request: String) async throws(APIError) -> Call
    func credentials(for call: Call, endpoint: EndpointRef) async throws(APIError) -> CallCredentials
    func report(
        _ call: Call, _ report: ConnectionReport, mediaVersion: Int64?, endpoint: EndpointRef, request: String
    ) async throws(APIError) -> Call
    func cleanup(_ call: Call, mediaVersion: Int64?, endpoint: EndpointRef, request: String) async throws(APIError)
    func call(_ id: String) async throws(APIError) -> Call
    func invite(_ user: String, to call: Call, endpoint: EndpointRef, request: String) async throws(APIError) -> Call
    func remove(_ user: String, from call: String, endpoint: EndpointRef, request: String) async throws(APIError) -> Call
}

protocol TournamentsAPI: Sendable {
    func list() async throws(APIError) -> [Tournament]
    func tournament(_ id: String) async throws(APIError) -> Tournament
    func create(_ spec: NewTournament, request: String) async throws(APIError) -> Tournament
    func perform(_ action: TournamentAction, on id: String, reason: String?) async throws(APIError) -> Tournament
}

protocol AnalysisAPI: Sendable {
    func analysis(of session: String) async throws(APIError) -> GameAnalysis?
}

enum GameSubscription: Equatable, Hashable, Sendable {
    case session(String)
    case match(GameKind)
}

enum ChannelState: Equatable, Sendable {
    case connecting(attempt: Int)
    case open
    case closed
}

enum ChannelEvent: Equatable, Sendable {
    case state(ChannelState)
    case session(Session)
    case error(SessionError)
    case match(MatchTicket)
}

protocol GameChannel: AnyObject, Sendable {
    var events: AsyncStream<ChannelEvent> { get }
    func send(_ command: SessionCommand) async
    func resync() async
    func close() async
}

protocol GameChannelFactory: Sendable {
    func open(_ subscription: GameSubscription) -> any GameChannel
}

struct Services: Sendable {
    let auth: any AuthAPI
    let game: any GameAPI
    let chat: any ChatAPI
    let calls: any CallsAPI
    let channels: any GameChannelFactory
    let chatSocket: any ChatSocketFactory
    let media: any MediaAPI
    let tournaments: any TournamentsAPI
    let analysis: any AnalysisAPI
}
