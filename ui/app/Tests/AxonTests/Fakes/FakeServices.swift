import Foundation
@testable import Axon

struct Registration: Equatable {
    let email: String
    let username: String
    let password: String
}

final class FakeAuth: AuthAPI, @unchecked Sendable {
    var restored: User?
    var loginResult: Result<User, APIError> = .failure(APIError(.unauthenticated))
    var registerResult: Result<User, APIError> = .failure(APIError(.alreadyExists))
    var profileMap: [String: PublicProfile] = [:]
    var logoutCount = 0
    var lastRegistration: Registration?
    let signedOut: AsyncStream<Void>
    let signOuts: AsyncStream<Void>.Continuation

    init() {
        (signedOut, signOuts) = AsyncStream.makeStream()
    }

    func restore() async -> User? { restored }

    func register(email: String, username: String, password: String) async throws(APIError) -> User {
        lastRegistration = Registration(email: email, username: username, password: password)
        return try registerResult.get()
    }

    func login(email: String, password: String) async throws(APIError) -> User {
        try loginResult.get()
    }

    func logout() async { logoutCount += 1 }

    func profiles(ids: [String]) async throws(APIError) -> [String: PublicProfile] {
        profileMap.filter { ids.contains($0.key) }
    }
}

final class FakeGame: GameAPI, @unchecked Sendable {
    var sessions: [String: Session] = [:]
    var mine: [Session] = []
    var live: [Session] = []
    var created: Session?
    var joined: Session?
    var failure: APIError?
    var ticket = MatchTicket()
    var cancelled: [GameKind] = []
    var leaveResult: Session?
    var historyResult: SessionHistory?
    var joinedIDs: [String] = []

    func createSession(_ game: GameKind) async throws(APIError) -> Session {
        if let failure { throw failure }
        guard let created else { throw APIError(.unavailable) }
        return created
    }

    func joinSession(id: String) async throws(APIError) -> Session {
        joinedIDs.append(id)
        if let failure { throw failure }
        guard let joined else { throw APIError(.notFound) }
        return joined
    }

    func session(id: String) async throws(APIError) -> Session {
        if let failure { throw failure }
        guard let session = sessions[id] else { throw APIError(.notFound) }
        return session
    }

    func mySessions() async throws(APIError) -> [Session] {
        if let failure { throw failure }
        return mine
    }

    func liveSessions(_ game: GameKind?) async throws(APIError) -> [Session] {
        if let failure { throw failure }
        return live
    }

    func findMatch(_ game: GameKind) async throws(APIError) -> MatchTicket {
        if let failure { throw failure }
        return ticket
    }

    func match(_ game: GameKind) async throws(APIError) -> MatchTicket { ticket }

    func cancelMatch(_ game: GameKind) async throws(APIError) {
        cancelled.append(game)
    }

    func leaveSession(id: String) async throws(APIError) -> Session? {
        if let failure { throw failure }
        return leaveResult
    }

    func history(id: String) async throws(APIError) -> SessionHistory {
        if let failure { throw failure }
        guard let historyResult else { throw APIError(.notFound) }
        return historyResult
    }
}

final class FakeChannel: GameChannel, @unchecked Sendable {
    let events: AsyncStream<ChannelEvent>
    private let continuation: AsyncStream<ChannelEvent>.Continuation
    var sent: [SessionCommand] = []
    var resyncs = 0
    var closed = false

    init() {
        (events, continuation) = AsyncStream.makeStream()
    }

    func emit(_ event: ChannelEvent) {
        continuation.yield(event)
    }

    func send(_ command: SessionCommand) async { sent.append(command) }
    func resync() async { resyncs += 1 }

    func close() async {
        closed = true
        continuation.finish()
    }
}

final class FakeChannels: GameChannelFactory, @unchecked Sendable {
    var opened: [GameSubscription] = []
    var channels: [FakeChannel] = []

    func open(_ subscription: GameSubscription) -> any GameChannel {
        let channel = FakeChannel()
        opened.append(subscription)
        channels.append(channel)
        return channel
    }

    var last: FakeChannel? { channels.last }
}

struct FakeWorld {
    let auth = FakeAuth()
    let game = FakeGame()
    let channels = FakeChannels()

    var services: Services { Services(auth: auth, game: game, channels: channels) }
}

enum GameFixtures {
    static let me = "11111111-1111-1111-1111-111111111111"
    static let rival = "22222222-2222-2222-2222-222222222222"
    static let third = "33333333-3333-3333-3333-333333333333"
    static let sessionID = "44444444-4444-4444-4444-444444444444"

    static func user(_ id: String = me, username: String = "ada") -> User {
        User.with {
            $0.id = id
            $0.email = "\(username)@axon.test"
            $0.username = username
        }
    }

    static func profile(_ id: String, _ name: String) -> PublicProfile {
        PublicProfile.with {
            $0.id = id
            $0.username = name
        }
    }

    static func session(
        id: String = sessionID,
        game: GameKind = .ticTacToe,
        players: [String] = [me, rival],
        status: SessionStatus = .playing,
        turn: String? = me,
        winner: String? = nil,
        revision: UInt32 = 0,
        updated: Int64 = 0
    ) -> Session {
        Session.with {
            $0.id = id
            $0.game = game.rawValue
            $0.playerIds = players
            $0.status = status
            $0.ply = revision
            $0.revision = revision
            if let turn { $0.turnUserID = turn }
            if let winner { $0.winnerUserID = winner }
            $0.ticTacToe = TicTacToeBoardState.with { $0.cells = Array(repeating: .unspecified, count: 9) }
            $0.updatedAt = .init(seconds: updated, nanos: 0)
        }
    }
}

@MainActor
func eventually(_ timeout: Duration = .seconds(2), _ condition: @MainActor () -> Bool) async -> Bool {
    let deadline = ContinuousClock.now + timeout
    while ContinuousClock.now < deadline {
        if condition() { return true }
        await Task.yield()
        try? await Task.sleep(for: .milliseconds(5))
    }
    return condition()
}
