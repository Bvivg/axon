import Foundation
import Synchronization

struct WebSocketClosure: Error, Equatable, Sendable {
    static let normal = 1000
    static let policyViolation = 1008
    static let resync = 4000

    let code: Int?
}

enum WebSocketConnectError: Error, Equatable, Sendable {
    case unauthorized
    case failed
}

protocol WebSocketConnection: AnyObject, Sendable {
    func send(_ text: String) async throws(WebSocketClosure)
    func receive() async throws(WebSocketClosure) -> String
    func close(code: Int)
}

protocol WebSocketTransport: Sendable {
    func connect(to url: URL, protocols: [String]) async throws(WebSocketConnectError) -> any WebSocketConnection
}

struct URLSessionWebSocketTransport: WebSocketTransport {
    func connect(to url: URL, protocols: [String]) async throws(WebSocketConnectError) -> any WebSocketConnection {
        let connection = URLSessionWebSocketConnection(url: url, protocols: protocols)
        try await connection.open()
        return connection
    }
}

final class URLSessionWebSocketConnection: WebSocketConnection {
    private let session: URLSession
    private let task: URLSessionWebSocketTask
    private let delegate: SocketDelegate

    init(url: URL, protocols: [String]) {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.httpShouldSetCookies = false
        configuration.httpCookieAcceptPolicy = .never
        configuration.httpCookieStorage = nil
        delegate = SocketDelegate()
        session = URLSession(configuration: configuration, delegate: delegate, delegateQueue: nil)
        task = session.webSocketTask(with: url, protocols: protocols)
    }

    deinit {
        session.invalidateAndCancel()
    }

    func open() async throws(WebSocketConnectError) {
        let delegate = delegate
        let task = task
        let result = await withTaskCancellationHandler {
            await withCheckedContinuation { continuation in
                delegate.awaitOpen(continuation)
                task.resume()
            }
        } onCancel: {
            delegate.resolve(.failure(.failed))
            task.cancel()
        }
        if case .failure(let error) = result {
            session.invalidateAndCancel()
            throw error
        }
    }

    func send(_ text: String) async throws(WebSocketClosure) {
        do {
            try await task.send(.string(text))
        } catch {
            throw closure()
        }
    }

    func receive() async throws(WebSocketClosure) -> String {
        while true {
            let message: URLSessionWebSocketTask.Message
            do {
                message = try await task.receive()
            } catch {
                throw closure()
            }
            switch message {
            case .string(let text):
                return text
            case .data(let data):
                if let text = String(data: data, encoding: .utf8) {
                    return text
                }
            @unknown default:
                continue
            }
        }
    }

    func close(code: Int) {
        let closeCode = URLSessionWebSocketTask.CloseCode(rawValue: code) ?? .normalClosure
        task.cancel(with: closeCode, reason: nil)
        session.finishTasksAndInvalidate()
    }

    private func closure() -> WebSocketClosure {
        session.finishTasksAndInvalidate()
        let code = task.closeCode == .invalid ? delegate.closeCode : task.closeCode.rawValue
        return WebSocketClosure(code: code)
    }
}

private final class SocketDelegate: NSObject, URLSessionWebSocketDelegate, Sendable {
    typealias Opening = CheckedContinuation<Result<Void, WebSocketConnectError>, Never>

    private enum Phase {
        case pending(Opening?)
        case settled(Result<Void, WebSocketConnectError>)
    }

    private let phase = Mutex(Phase.pending(nil))
    private let closedWith = Mutex<Int?>(nil)

    var closeCode: Int? { closedWith.withLock { $0 } }

    func awaitOpen(_ continuation: Opening) {
        let settled: Result<Void, WebSocketConnectError>? = phase.withLock { phase in
            switch phase {
            case .pending:
                phase = .pending(continuation)
                return nil
            case .settled(let result):
                return result
            }
        }
        if let settled {
            continuation.resume(returning: settled)
        }
    }

    func resolve(_ result: Result<Void, WebSocketConnectError>) {
        let waiting: Opening? = phase.withLock { phase in
            guard case .pending(let continuation) = phase else { return nil }
            phase = .settled(result)
            return continuation
        }
        waiting?.resume(returning: result)
    }

    func urlSession(
        _ session: URLSession,
        webSocketTask: URLSessionWebSocketTask,
        didOpenWithProtocol protocol: String?
    ) {
        resolve(.success(()))
    }

    func urlSession(
        _ session: URLSession,
        webSocketTask: URLSessionWebSocketTask,
        didCloseWith closeCode: URLSessionWebSocketTask.CloseCode,
        reason: Data?
    ) {
        closedWith.withLock { $0 = closeCode.rawValue }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: (any Error)?) {
        let status = (task.response as? HTTPURLResponse)?.statusCode
        resolve(.failure(status == 401 ? .unauthorized : .failed))
    }
}
