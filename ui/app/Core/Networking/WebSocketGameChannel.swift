import Foundation
import Synchronization

final class WebSocketGameChannel: GameChannel {
    static let subprotocol = "axon.game.v1"
    static let bearerPrefix = "axon.bearer."

    let events: AsyncStream<ChannelEvent>
    private let continuation: AsyncStream<ChannelEvent>.Continuation
    private let subscription: GameSubscription
    private let url: URL
    private let vault: TokenVault
    private let transport: any WebSocketTransport
    private let policy: ReconnectPolicy
    private let sleeper: any ChannelSleeper
    private let codec = SessionCodec()
    private let state = Mutex(State())

    private struct State {
        var generation = 0
        var loop: Task<Void, Never>?
        var connection: (any WebSocketConnection)?
        var open = false
        var finished = false
    }

    private enum Disconnect {
        case dropped(synced: Bool)
        case unauthorized(token: String, synced: Bool)
        case terminal

        var synced: Bool {
            switch self {
            case .dropped(let synced), .unauthorized(_, let synced): synced
            case .terminal: false
            }
        }
    }

    private enum Next {
        case reconnectNow
        case backOff
        case stop
    }

    private enum Opened {
        case ready(any WebSocketConnection, token: String)
        case failed(Disconnect)
    }

    init(
        subscription: GameSubscription,
        url: URL,
        vault: TokenVault,
        transport: any WebSocketTransport,
        policy: ReconnectPolicy = .standard,
        sleeper: any ChannelSleeper = TaskSleeper()
    ) {
        self.subscription = subscription
        self.url = url
        self.vault = vault
        self.transport = transport
        self.policy = policy
        self.sleeper = sleeper
        (events, continuation) = AsyncStream.makeStream()
        continuation.onTermination = { [weak self] _ in self?.shutdown() }
        restart()
    }

    func send(_ command: SessionCommand) async {
        let connection = state.withLock { $0.open ? $0.connection : nil }
        guard let connection, let text = try? codec.encode(command) else {
            emit(.error(.unavailable))
            return
        }
        do {
            try await connection.send(text)
        } catch {
            emit(.error(.unavailable))
        }
    }

    func resync() async {
        restart()
    }

    func close() async {
        shutdown()
    }

    private func restart() {
        let previous: (any WebSocketConnection)? = state.withLock { state in
            guard !state.finished else { return nil }
            state.generation += 1
            state.loop?.cancel()
            let previous = state.connection
            state.connection = nil
            state.open = false
            let generation = state.generation
            state.loop = Task { [weak self] in await self?.run(generation: generation) }
            return previous
        }
        previous?.close(code: WebSocketClosure.resync)
    }

    private func shutdown(generation expected: Int? = nil) {
        let stopped: (connection: (any WebSocketConnection)?, first: Bool)? = state.withLock { state in
            if let expected, state.generation != expected { return nil }
            let first = !state.finished
            state.finished = true
            state.generation += 1
            state.loop?.cancel()
            state.loop = nil
            let connection = state.connection
            state.connection = nil
            state.open = false
            return (connection, first)
        }
        guard let stopped else { return }
        stopped.connection?.close(code: WebSocketClosure.normal)
        guard stopped.first else { return }
        continuation.yield(.state(.closed))
        continuation.finish()
    }

    private func emit(_ event: ChannelEvent, generation: Int? = nil) {
        state.withLock { state in
            guard !state.finished, generation == nil || state.generation == generation else { return }
            continuation.yield(event)
        }
    }

    private func adopt(_ connection: any WebSocketConnection, generation: Int) -> Bool {
        state.withLock { state in
            guard !state.finished, state.generation == generation else { return false }
            state.connection = connection
            return true
        }
    }

    private func markOpen(_ connection: any WebSocketConnection, generation: Int) -> Bool {
        state.withLock { state in
            guard !state.finished, state.generation == generation, state.connection === connection else { return false }
            state.open = true
            continuation.yield(.state(.open))
            return true
        }
    }

    private func release(_ connection: any WebSocketConnection, generation: Int) {
        state.withLock { state in
            guard state.generation == generation, state.connection === connection else { return }
            state.connection = nil
            state.open = false
        }
    }

    private func run(generation: Int) async {
        var attempt = 0
        var renewed = false
        while !Task.isCancelled {
            emit(.state(.connecting(attempt: attempt)), generation: generation)
            let disconnect = await connectOnce(generation: generation)
            if Task.isCancelled { return }
            if disconnect.synced {
                attempt = 0
                renewed = false
            }
            let next = await decide(after: disconnect, renewed: &renewed)
            if Task.isCancelled { return }
            switch next {
            case .reconnectNow:
                continue
            case .stop:
                shutdown(generation: generation)
                return
            case .backOff:
                guard let delay = policy.delay(forAttempt: attempt) else {
                    shutdown(generation: generation)
                    return
                }
                attempt += 1
                do {
                    try await sleeper.sleep(for: delay)
                } catch {
                    return
                }
            }
        }
    }

    private func decide(after disconnect: Disconnect, renewed: inout Bool) async -> Next {
        switch disconnect {
        case .terminal:
            return .stop
        case .dropped:
            return .backOff
        case .unauthorized(let token, _):
            guard await vault.refresh(rejected: token) != nil else {
                return await vault.hasSession ? .backOff : .stop
            }
            if renewed { return .backOff }
            renewed = true
            return .reconnectNow
        }
    }

    private func connectOnce(generation: Int) async -> Disconnect {
        switch await open(generation: generation) {
        case .failed(let disconnect):
            return disconnect
        case .ready(let connection, let token):
            return await pump(connection, token: token, generation: generation)
        }
    }

    private func open(generation: Int) async -> Opened {
        var token = await vault.validAccessToken()
        if token == nil {
            token = await vault.accessToken
        }
        guard let token, let frame = try? codec.encode(SessionCodec.subscribe(subscription)) else {
            return .failed(.terminal)
        }
        let protocols = [Self.subprotocol, Self.bearerPrefix + token]
        let connection: any WebSocketConnection
        do {
            connection = try await transport.connect(to: url, protocols: protocols)
        } catch .unauthorized {
            return .failed(.unauthorized(token: token, synced: false))
        } catch {
            return .failed(.dropped(synced: false))
        }
        guard adopt(connection, generation: generation) else {
            connection.close(code: WebSocketClosure.normal)
            return .failed(.dropped(synced: false))
        }
        do {
            try await connection.send(frame)
        } catch {
            release(connection, generation: generation)
            return .failed(Self.disconnect(error, token: token, synced: false))
        }
        guard markOpen(connection, generation: generation) else {
            connection.close(code: WebSocketClosure.normal)
            return .failed(.dropped(synced: false))
        }
        return .ready(connection, token: token)
    }

    private func pump(_ connection: any WebSocketConnection, token: String, generation: Int) async -> Disconnect {
        var synced = false
        while true {
            let text: String
            do {
                text = try await connection.receive()
            } catch {
                release(connection, generation: generation)
                return Self.disconnect(error, token: token, synced: synced)
            }
            guard let event = codec.event(from: text) else { continue }
            if case .session = event { synced = true }
            if case .match = event { synced = true }
            emit(event, generation: generation)
        }
    }

    private static func disconnect(_ closure: WebSocketClosure, token: String, synced: Bool) -> Disconnect {
        if closure.code == WebSocketClosure.policyViolation {
            return .unauthorized(token: token, synced: synced)
        }
        return .dropped(synced: synced)
    }
}

struct WebSocketGameChannelFactory: GameChannelFactory {
    let url: URL
    let vault: TokenVault
    var transport: any WebSocketTransport = URLSessionWebSocketTransport()
    var policy: ReconnectPolicy = .standard
    var sleeper: any ChannelSleeper = TaskSleeper()

    func open(_ subscription: GameSubscription) -> any GameChannel {
        WebSocketGameChannel(
            subscription: subscription,
            url: url,
            vault: vault,
            transport: transport,
            policy: policy,
            sleeper: sleeper
        )
    }
}
