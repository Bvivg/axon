import Foundation
import Synchronization

enum RefreshOutcome: Sendable, Equatable {
    case rotated(AuthTokens)
    case rejected
    case unavailable
}

actor TokenVault {
    typealias Refresher = @Sendable (String) async -> RefreshOutcome

    private let store: any AuthTokenStore
    private let now: @Sendable () -> Date
    private var tokens: AuthTokens?
    private var refreshing: Task<String?, Never>?
    private var epoch = 0
    private nonisolated let refresher = Mutex<Refresher?>(nil)
    private let signOuts: AsyncStream<Void>.Continuation
    nonisolated let signedOut: AsyncStream<Void>

    init(store: any AuthTokenStore, now: @escaping @Sendable () -> Date = { .now }) {
        self.store = store
        self.now = now
        tokens = try? store.load()
        (signedOut, signOuts) = AsyncStream.makeStream(bufferingPolicy: .bufferingNewest(1))
    }

    var hasSession: Bool { tokens != nil }

    var accessToken: String? { tokens?.accessToken }

    var refreshToken: String? { tokens?.refreshToken }

    nonisolated func setRefresher(_ refresher: @escaping Refresher) {
        self.refresher.withLock { $0 = refresher }
    }

    func store(_ tokens: AuthTokens) {
        epoch += 1
        refreshing = nil
        self.tokens = tokens
        try? store.save(tokens)
    }

    func clear() {
        epoch += 1
        refreshing = nil
        tokens = nil
        try? store.clear()
    }

    func validAccessToken(margin: TimeInterval = 30) async -> String? {
        guard let tokens else { return nil }
        guard tokens.expires(within: margin, now: now()) else { return tokens.accessToken }
        return await refresh(rejected: tokens.accessToken)
    }

    func refresh(rejected: String?) async -> String? {
        if let rejected, let current = tokens?.accessToken, current != rejected {
            return current
        }
        if let refreshing {
            return await refreshing.value
        }
        guard let refreshToken = tokens?.refreshToken, let refresher = refresher.withLock({ $0 }) else { return nil }
        let started = epoch
        let task = Task { self.complete(await refresher(refreshToken), epoch: started) }
        refreshing = task
        return await task.value
    }

    private func complete(_ outcome: RefreshOutcome, epoch started: Int) -> String? {
        guard epoch == started else { return tokens?.accessToken }
        refreshing = nil
        switch outcome {
        case .rotated(let rotated):
            store(rotated)
            return rotated.accessToken
        case .rejected:
            clear()
            signOuts.yield()
            return nil
        case .unavailable:
            return nil
        }
    }
}
