import Foundation

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
    private var refreshing: Task<RefreshOutcome, Never>?
    private var refresher: Refresher?
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

    func setRefresher(_ refresher: @escaping Refresher) {
        self.refresher = refresher
    }

    func store(_ tokens: AuthTokens) {
        self.tokens = tokens
        try? store.save(tokens)
    }

    func clear() {
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
            return accessToken(after: await refreshing.value)
        }
        guard let refreshToken = tokens?.refreshToken, let refresher else { return nil }
        let task = Task { await refresher(refreshToken) }
        refreshing = task
        let outcome = await task.value
        refreshing = nil
        switch outcome {
        case .rotated(let rotated):
            store(rotated)
        case .rejected:
            clear()
            signOuts.yield()
        case .unavailable:
            break
        }
        return accessToken(after: outcome)
    }

    private func accessToken(after outcome: RefreshOutcome) -> String? {
        if case .rotated(let rotated) = outcome { return rotated.accessToken }
        return nil
    }
}
