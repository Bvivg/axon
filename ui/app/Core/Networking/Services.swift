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

protocol ChatAPI: Sendable {
    func rooms() async throws(APIError) -> [Room]
}

protocol CallsAPI: Sendable {
    func history(limit: Int) async throws(APIError) -> [Call]
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
}
