import Foundation

actor ChatPeerSocket {
    static let chatProtocol = "axon.chat.v1"

    private let task: URLSessionWebSocketTask
    private var pending: [[String: Any]] = []
    private var closedBy: Error?
    private var closed = false
    private var reader: Task<Void, Never>?

    init(url: URL, token: String, session: URLSession) {
        task = session.webSocketTask(with: url, protocols: [Self.chatProtocol, GameSocket.bearerPrefix + token])
    }

    func start() {
        task.resume()
        reader = Task { await self.read() }
    }

    func close() {
        task.cancel(with: .normalClosure, reason: nil)
        reader?.cancel()
    }

    func send(_ body: String, to room: String, timeout: Duration = .seconds(20)) async throws {
        let clientID = UUID().uuidString.lowercased()
        let frame: [String: Any] = ["type": "send", "client_id": clientID, "room_id": room, "body": body]
        let data = try JSONSerialization.data(withJSONObject: frame)
        try await task.send(.string(String(bytes: data, encoding: .utf8) ?? ""))
        try await next("the ack for \(body)", timeout: timeout) {
            $0["type"] as? String == "ack" && $0["client_id"] as? String == clientID
        }
    }

    private func next(_ waiting: String, timeout: Duration, where matches: ([String: Any]) -> Bool) async throws {
        let deadline = ContinuousClock.now.advanced(by: timeout)
        while true {
            while !pending.isEmpty {
                let frame = pending.removeFirst()
                if matches(frame) { return }
                if frame["type"] as? String == "error" {
                    throw OpponentError.chatError(frame["code"] as? String ?? "unknown", waiting)
                }
            }
            if closed {
                throw OpponentError.socketClosed(waiting, closedBy)
            }
            guard ContinuousClock.now < deadline else {
                throw OpponentError.timedOut(waiting)
            }
            try await Task.sleep(for: .milliseconds(50))
        }
    }

    private func read() async {
        while !Task.isCancelled {
            do {
                let data: Data
                switch try await task.receive() {
                case .string(let string): data = Data(string.utf8)
                case .data(let bytes): data = bytes
                @unknown default: continue
                }
                if let frame = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] {
                    pending.append(frame)
                }
            } catch {
                closedBy = error
                break
            }
        }
        closed = true
    }
}
