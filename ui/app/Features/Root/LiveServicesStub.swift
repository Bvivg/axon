import Foundation

enum LiveServices {
    @MainActor
    static func make(settings: GatewaySettings, vault: TokenVault) -> Services {
        Services(auth: UnavailableAuth(), game: UnavailableGame(), channels: UnavailableChannels())
    }
}

private struct UnavailableAuth: AuthAPI {
    func restore() async -> User? { nil }
    func register(email: String, username: String, password: String) async throws(APIError) -> User { throw APIError(.unavailable) }
    func login(email: String, password: String) async throws(APIError) -> User { throw APIError(.unavailable) }
    func logout() async {}
    func profiles(ids: [String]) async throws(APIError) -> [String: PublicProfile] { [:] }
    var signedOut: AsyncStream<Void> { AsyncStream { $0.finish() } }
}

private struct UnavailableGame: GameAPI {
    func createSession(_ game: GameKind) async throws(APIError) -> Session { throw APIError(.unavailable) }
    func joinSession(id: String) async throws(APIError) -> Session { throw APIError(.unavailable) }
    func session(id: String) async throws(APIError) -> Session { throw APIError(.unavailable) }
    func mySessions() async throws(APIError) -> [Session] { [] }
    func liveSessions(_ game: GameKind?) async throws(APIError) -> [Session] { [] }
    func findMatch(_ game: GameKind) async throws(APIError) -> MatchTicket { throw APIError(.unavailable) }
    func match(_ game: GameKind) async throws(APIError) -> MatchTicket { throw APIError(.unavailable) }
    func cancelMatch(_ game: GameKind) async throws(APIError) {}
    func leaveSession(id: String) async throws(APIError) -> Session? { nil }
    func history(id: String) async throws(APIError) -> SessionHistory { throw APIError(.unavailable) }
}

private final class UnavailableChannel: GameChannel {
    let events = AsyncStream<ChannelEvent> { $0.finish() }
    func send(_ command: SessionCommand) async {}
    func resync() async {}
    func close() async {}
}

private struct UnavailableChannels: GameChannelFactory {
    func open(_ subscription: GameSubscription) -> any GameChannel { UnavailableChannel() }
}
