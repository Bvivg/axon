import Foundation
import Synchronization

protocol ChatSocket: AnyObject, Sendable {
    var events: AsyncStream<ChatEvent> { get }
    func send(_ command: ChatCommand) async -> Bool
    func reconnect() async
    func close() async
}

protocol ChatSocketFactory: Sendable {
    func open() -> any ChatSocket
}

final class WebSocketChatSocket: ChatSocket {
    static let subprotocol = "axon.chat.v1"
    static let unauthenticated = 4401
    static let tokenExpired = 4402
    static let sessionRevoked = 4403

    let events: AsyncStream<ChatEvent>
    private let continuation: AsyncStream<ChatEvent>.Continuation
    private let url: URL
    private let vault: TokenVault
    private let transport: any WebSocketTransport
    private let policy: ReconnectPolicy
    private let sleeper: any ChannelSleeper
    private let state = Mutex(State())

    private struct State {
        var generation = 0
        var loop: Task<Void, Never>?
        var connection: (any WebSocketConnection)?
        var open = false
        var finished = false
    }

    private enum Ending {
        case dropped(heard: Bool)
        case unauthorized(token: String, heard: Bool)
        case revoked
        case stopped
    }

    init(
        url: URL,
        vault: TokenVault,
        transport: any WebSocketTransport,
        policy: ReconnectPolicy = .standard,
        sleeper: any ChannelSleeper = TaskSleeper()
    ) {
        self.url = url
        self.vault = vault
        self.transport = transport
        self.policy = policy
        self.sleeper = sleeper
        (events, continuation) = AsyncStream.makeStream()
        continuation.onTermination = { [weak self] _ in self?.shutdown() }
        restart()
    }

    func send(_ command: ChatCommand) async -> Bool {
        let connection = state.withLock { $0.open ? $0.connection : nil }
        guard let connection, let text = ChatWire.encode(command) else { return false }
        do {
            try await connection.send(text)
            return true
        } catch {
            return false
        }
    }

    func reconnect() async {
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

    private func emit(_ event: ChatEvent, generation: Int) {
        state.withLock { state in
            guard !state.finished, state.generation == generation else { return }
            continuation.yield(event)
        }
    }

    private enum Next {
        case retryNow
        case backOff(resetting: Bool)
        case stop
    }

    private func run(generation: Int) async {
        var attempt = 0
        var renewed = false
        while !Task.isCancelled {
            emit(.state(.connecting(attempt: attempt)), generation: generation)
            let ending = await connectOnce(generation: generation)
            if Task.isCancelled { return }
            switch await next(after: ending, renewed: &renewed, generation: generation) {
            case .retryNow:
                continue
            case .stop:
                shutdown(generation: generation)
                return
            case .backOff(let resetting):
                if resetting { attempt = 0 }
            }
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

    private func next(after ending: Ending, renewed: inout Bool, generation: Int) async -> Next {
        switch ending {
        case .stopped:
            return .stop
        case .revoked:
            emit(.revoked, generation: generation)
            return .stop
        case .dropped(let heard):
            if heard { renewed = false }
            return .backOff(resetting: heard)
        case .unauthorized(let token, let heard):
            if await vault.refresh(rejected: token) != nil, !renewed {
                renewed = true
                return .retryNow
            }
            return await vault.hasSession ? .backOff(resetting: heard) : .stop
        }
    }

    private func connectOnce(generation: Int) async -> Ending {
        var token = await vault.validAccessToken()
        if token == nil { token = await vault.accessToken }
        guard let token else { return .stopped }
        let connection: any WebSocketConnection
        do {
            connection = try await transport.connect(to: url, protocols: [Self.subprotocol, WebSocketGameChannel.bearerPrefix + token])
        } catch .unauthorized {
            return .unauthorized(token: token, heard: false)
        } catch {
            return .dropped(heard: false)
        }
        let adopted = state.withLock { state in
            guard !state.finished, state.generation == generation else { return false }
            state.connection = connection
            state.open = true
            continuation.yield(.state(.open))
            return true
        }
        guard adopted else {
            connection.close(code: WebSocketClosure.normal)
            return .dropped(heard: false)
        }
        var heard = false
        while true {
            let text: String
            do {
                text = try await connection.receive()
            } catch {
                state.withLock { state in
                    guard state.generation == generation, state.connection === connection else { return }
                    state.connection = nil
                    state.open = false
                }
                return Self.ending(error.code, token: token, heard: heard)
            }
            guard let event = ChatWire.event(from: text) else { continue }
            heard = true
            emit(event, generation: generation)
        }
    }

    private static func ending(_ code: Int?, token: String, heard: Bool) -> Ending {
        switch code {
        case unauthenticated, tokenExpired, WebSocketClosure.policyViolation:
            .unauthorized(token: token, heard: heard)
        case sessionRevoked:
            .revoked
        default:
            .dropped(heard: heard)
        }
    }
}

struct WebSocketChatSocketFactory: ChatSocketFactory {
    let url: URL
    let vault: TokenVault
    var transport: any WebSocketTransport = URLSessionWebSocketTransport()
    var policy: ReconnectPolicy = .persistent
    var sleeper: any ChannelSleeper = TaskSleeper()

    func open() -> any ChatSocket {
        WebSocketChatSocket(url: url, vault: vault, transport: transport, policy: policy, sleeper: sleeper)
    }
}
