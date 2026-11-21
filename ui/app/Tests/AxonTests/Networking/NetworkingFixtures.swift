import Connect
import Foundation
import Synchronization
@testable import Axon

final class CountingRefresher: Sendable {
    private let outcomes: Mutex<[RefreshOutcome]>
    private let seen = Mutex<[String]>([])

    init(_ outcomes: RefreshOutcome...) {
        self.outcomes = Mutex(outcomes)
    }

    var calls: [String] { seen.withLock { $0 } }

    func refresh(_ refreshToken: String) async -> RefreshOutcome {
        seen.withLock { $0.append(refreshToken) }
        await Task.yield()
        return outcomes.withLock { $0.isEmpty ? .unavailable : $0.removeFirst() }
    }
}

final class VolatileSettingsStorage: SettingsStorage {
    private let values = Mutex<[String: String]>([:])

    func string(forKey key: String) -> String? {
        values.withLock { $0[key] }
    }

    func set(_ value: String?, forKey key: String) {
        values.withLock { $0[key] = value }
    }
}

enum Fixtures {
    static func tokens(_ access: String, refresh: String? = nil, expiresIn: TimeInterval = 900) -> AuthTokens {
        AuthTokens(
            accessToken: access,
            refreshToken: refresh ?? "refresh-\(access)",
            expiresAt: .now.addingTimeInterval(expiresIn)
        )
    }

    static func pair(_ access: String, refresh: String, expiresIn: Int64 = 900) -> TokenPair {
        var pair = TokenPair()
        pair.accessToken = access
        pair.refreshToken = refresh
        pair.expiresIn = expiresIn
        pair.tokenType = "Bearer"
        return pair
    }

    static func vault(_ tokens: AuthTokens?, refresher: CountingRefresher? = nil) -> TokenVault {
        let vault = TokenVault(store: InMemoryTokenStore(tokens))
        if let refresher {
            vault.setRefresher { await refresher.refresh($0) }
        }
        return vault
    }

    static func session(_ id: String, revision: UInt32 = 0) -> Session {
        var session = Session()
        session.id = id
        session.game = GameKind.ticTacToe.rawValue
        session.playerIds = ["u-1", "u-2"]
        session.status = .playing
        session.revision = revision
        return session
    }

    static func sessionFrame(_ id: String, revision: UInt32 = 0) -> String {
        var event = SessionEvent()
        event.session = session(id, revision: revision)
        return (try? event.jsonString()) ?? ""
    }

    static func user(_ id: String) -> User {
        var user = User()
        user.id = id
        user.email = "\(id)@axon.test"
        return user
    }

    static func denied(_ code: Connect.Code, _ message: String = "denied") -> ConnectError {
        ConnectError(code: code, message: message)
    }

    static let socketURL = URL(string: "ws://localhost:18080/ws/game")!
}
