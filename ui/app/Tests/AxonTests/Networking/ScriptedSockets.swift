import Foundation
import Synchronization
@testable import Axon

final class ScriptedSocket: WebSocketConnection {
    private let inbox: AsyncStream<Result<String, WebSocketClosure>>
    private let feed: AsyncStream<Result<String, WebSocketClosure>>.Continuation
    private let outbox = Mutex<[String]>([])
    private let closedWith = Mutex<Int?>(nil)

    init() {
        (inbox, feed) = AsyncStream.makeStream()
    }

    var sent: [String] { outbox.withLock { $0 } }

    var closeCode: Int? { closedWith.withLock { $0 } }

    func deliver(_ text: String) {
        feed.yield(.success(text))
    }

    func drop(code: Int?) {
        feed.yield(.failure(WebSocketClosure(code: code)))
        feed.finish()
    }

    func send(_ text: String) async throws(WebSocketClosure) {
        if let code = closeCode { throw WebSocketClosure(code: code) }
        outbox.withLock { $0.append(text) }
    }

    func receive() async throws(WebSocketClosure) -> String {
        var iterator = inbox.makeAsyncIterator()
        switch await iterator.next() {
        case .success(let text): return text
        case .failure(let closure): throw closure
        case nil: throw WebSocketClosure(code: closeCode)
        }
    }

    func close(code: Int) {
        closedWith.withLock { $0 = $0 ?? code }
        feed.yield(.failure(WebSocketClosure(code: code)))
        feed.finish()
    }
}

final class ScriptedSocketTransport: WebSocketTransport {
    enum Step: Sendable {
        case connect(ScriptedSocket)
        case fail(WebSocketConnectError)
    }

    private let steps: Mutex<[Step]>
    private let offers = Mutex<[[String]]>([])

    init(_ steps: Step...) {
        self.steps = Mutex(steps)
    }

    var attempts: [[String]] { offers.withLock { $0 } }

    var bearers: [String] {
        attempts.compactMap { protocols in
            protocols.first { $0.hasPrefix(WebSocketGameChannel.bearerPrefix) }
                .map { String($0.dropFirst(WebSocketGameChannel.bearerPrefix.count)) }
        }
    }

    func connect(to url: URL, protocols: [String]) async throws(WebSocketConnectError) -> any WebSocketConnection {
        offers.withLock { $0.append(protocols) }
        let step: Step? = steps.withLock { $0.isEmpty ? nil : $0.removeFirst() }
        switch step {
        case .connect(let socket):
            return socket
        case .fail(let error):
            throw error
        case nil:
            let (never, _) = AsyncStream<Void>.makeStream()
            for await _ in never {}
            throw .failed
        }
    }
}

final class RecordingSleeper: ChannelSleeper {
    private let slept = Mutex<[Duration]>([])

    var delays: [Duration] { slept.withLock { $0 } }

    func sleep(for duration: Duration) async throws {
        try Task.checkCancellation()
        slept.withLock { $0.append(duration) }
    }
}

struct EventReader {
    private var iterator: AsyncStream<ChannelEvent>.AsyncIterator

    init(_ channel: any GameChannel) {
        iterator = channel.events.makeAsyncIterator()
    }

    mutating func next() async -> ChannelEvent? {
        await iterator.next()
    }

    mutating func take(_ count: Int) async -> [ChannelEvent] {
        var events: [ChannelEvent] = []
        while events.count < count, let event = await iterator.next() {
            events.append(event)
        }
        return events
    }

    mutating func until(_ predicate: (ChannelEvent) -> Bool) async -> [ChannelEvent] {
        var events: [ChannelEvent] = []
        while let event = await iterator.next() {
            events.append(event)
            if predicate(event) { break }
        }
        return events
    }

    mutating func rest() async -> [ChannelEvent] {
        var events: [ChannelEvent] = []
        while let event = await iterator.next() {
            events.append(event)
        }
        return events
    }
}

struct ChannelHarness {
    let sleeper = RecordingSleeper()

    func channel(
        _ subscription: GameSubscription = .session("s-1"),
        vault: TokenVault,
        transport: ScriptedSocketTransport,
        policy: ReconnectPolicy = ReconnectPolicy(jitter: 0)
    ) -> WebSocketGameChannel {
        WebSocketGameChannel(
            subscription: subscription,
            url: Fixtures.socketURL,
            vault: vault,
            transport: transport,
            policy: policy,
            sleeper: sleeper
        )
    }

    static func isOpen(_ event: ChannelEvent) -> Bool { event == .state(.open) }

    static func isSession(_ event: ChannelEvent) -> Bool {
        if case .session = event { return true }
        return false
    }
}
