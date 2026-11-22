import Foundation
import SwiftProtobuf

typealias GameSession = Axon_Game_V1_Session
typealias GameSessionCommand = Axon_Game_V1_SessionCommand
typealias GameSessionEvent = Axon_Game_V1_SessionEvent

enum OpponentError: Error, CustomStringConvertible {
    case invalidGateway(String)
    case rpc(method: String, status: Int, body: String)
    case socketClosed(String, Error?)
    case sessionError(Axon_Game_V1_SessionError, String)
    case chatError(String, String)
    case timedOut(String)

    var description: String {
        switch self {
        case .invalidGateway(let raw):
            "invalid gateway URL \(raw)"
        case let .rpc(method, status, body):
            "\(method) answered \(status): \(body)"
        case let .socketClosed(waiting, error):
            "game socket closed while waiting for \(waiting): \(error.map { "\($0)" } ?? "no error")"
        case let .sessionError(error, waiting):
            "game socket error \(error) while waiting for \(waiting)"
        case let .chatError(code, waiting):
            "chat socket error \(code) while waiting for \(waiting)"
        case .timedOut(let waiting):
            "timed out waiting for \(waiting)"
        }
    }
}

enum E2EGateway {
    static let environmentKey = "AXON_E2E_GATEWAY"
    static let fallback = "http://localhost:18680"

    static func baseURL() throws -> URL {
        let raw = ProcessInfo.processInfo.environment[environmentKey] ?? fallback
        guard let url = URL(string: raw), url.scheme == "http" || url.scheme == "https" else {
            throw OpponentError.invalidGateway(raw)
        }
        return url
    }

    static func socketURL(_ base: URL, path: String = "/ws/game") throws -> URL {
        guard var components = URLComponents(url: base, resolvingAgainstBaseURL: false) else {
            throw OpponentError.invalidGateway(base.absoluteString)
        }
        components.scheme = components.scheme == "https" ? "wss" : "ws"
        components.path = path
        guard let url = components.url else {
            throw OpponentError.invalidGateway(base.absoluteString)
        }
        return url
    }
}

struct TestAccount: Sendable {
    let email: String
    let username: String
    let password: String

    static func random(prefix: String) -> TestAccount {
        let suffix = UUID().uuidString.lowercased().replacingOccurrences(of: "-", with: "").prefix(12)
        let username = "\(prefix)_\(suffix)"
        return TestAccount(email: "\(username)@axon.test", username: username, password: "Axon-e2e-\(UUID().uuidString)")
    }
}

enum WireJSON {
    static var decoding: JSONDecodingOptions {
        var options = JSONDecodingOptions()
        options.ignoreUnknownFields = true
        return options
    }
}

actor Opponent {
    let baseURL: URL
    let account: TestAccount
    private(set) var userID = ""
    private var accessToken = ""
    private let http: URLSession

    init(baseURL: URL, account: TestAccount = .random(prefix: "opp")) {
        self.baseURL = baseURL
        self.account = account
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForRequest = 15
        http = URLSession(configuration: configuration)
    }

    @discardableResult
    func register() async throws -> Axon_Auth_V1_User {
        var request = Axon_Auth_V1_RegisterRequest()
        request.email = account.email
        request.password = account.password
        request.username = account.username
        let response: Axon_Auth_V1_RegisterResponse = try await call("axon.auth.v1.AuthService/Register", request)
        adopt(response.user, response.tokens)
        return response.user
    }

    @discardableResult
    func login() async throws -> Axon_Auth_V1_User {
        var request = Axon_Auth_V1_LoginRequest()
        request.email = account.email
        request.password = account.password
        let response: Axon_Auth_V1_LoginResponse = try await call("axon.auth.v1.AuthService/Login", request)
        adopt(response.user, response.tokens)
        return response.user
    }

    func createSession(game: String) async throws -> GameSession {
        var request = Axon_Game_V1_CreateSessionRequest()
        request.game = game
        request.requestID = UUID().uuidString
        let response: Axon_Game_V1_CreateSessionResponse = try await call("axon.game.v1.GameService/CreateSession", request)
        return response.session
    }

    func joinSession(_ id: String) async throws -> GameSession {
        var request = Axon_Game_V1_JoinSessionRequest()
        request.sessionID = id
        let response: Axon_Game_V1_JoinSessionResponse = try await call("axon.game.v1.GameService/JoinSession", request)
        return response.session
    }

    func getSession(_ id: String) async throws -> GameSession {
        var request = Axon_Game_V1_GetSessionRequest()
        request.sessionID = id
        let response: Axon_Game_V1_GetSessionResponse = try await call("axon.game.v1.GameService/GetSession", request)
        return response.session
    }

    func connect(to sessionID: String) async throws -> GameSocket {
        let socket = GameSocket(url: try E2EGateway.socketURL(baseURL), token: accessToken, session: http)
        await socket.start()
        try await socket.subscribe(sessionID)
        return socket
    }

    func rooms() async throws -> [Axon_Chat_V1_Room] {
        let response: Axon_Chat_V1_ListRoomsResponse = try await call("axon.chat.v1.ChatService/ListRooms", Axon_Chat_V1_ListRoomsRequest())
        return response.rooms
    }

    func room(withLastMessage body: String, timeout: Duration = .seconds(20)) async throws -> Axon_Chat_V1_Room {
        let deadline = ContinuousClock.now.advanced(by: timeout)
        while ContinuousClock.now < deadline {
            if let room = try await rooms().first(where: { $0.hasLastMessage && $0.lastMessage.body == body }) {
                return room
            }
            try await Task.sleep(for: .milliseconds(300))
        }
        throw OpponentError.timedOut("a room whose last message is \(body)")
    }

    func connectChat() async throws -> ChatPeerSocket {
        let socket = ChatPeerSocket(url: try E2EGateway.socketURL(baseURL, path: "/ws/chat"), token: accessToken, session: http)
        await socket.start()
        return socket
    }

    private func adopt(_ user: Axon_Auth_V1_User, _ tokens: Axon_Auth_V1_TokenPair) {
        userID = user.id
        accessToken = tokens.accessToken
    }

    private func call<Request: SwiftProtobuf.Message, Response: SwiftProtobuf.Message>(
        _ method: String,
        _ message: Request
    ) async throws -> Response {
        var request = URLRequest(url: baseURL.appending(path: method))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("1", forHTTPHeaderField: "Connect-Protocol-Version")
        if !accessToken.isEmpty {
            request.setValue("Bearer \(accessToken)", forHTTPHeaderField: "Authorization")
        }
        request.httpBody = Data(try message.jsonString().utf8)
        let (data, response) = try await http.data(for: request)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else {
            let body = String(bytes: data, encoding: .utf8) ?? "\(data.count) bytes"
            throw OpponentError.rpc(method: method, status: status, body: body)
        }
        return try Response(jsonUTF8Data: data, options: WireJSON.decoding)
    }
}

actor GameSocket {
    static let gameProtocol = "axon.game.v1"
    static let bearerPrefix = "axon.bearer."

    private let task: URLSessionWebSocketTask
    private var pending: [GameSessionEvent] = []
    private var closedBy: Error?
    private var closed = false
    private var reader: Task<Void, Never>?
    private(set) var latest: GameSession?

    init(url: URL, token: String, session: URLSession) {
        task = session.webSocketTask(with: url, protocols: [Self.gameProtocol, Self.bearerPrefix + token])
    }

    func start() {
        task.resume()
        reader = Task { await self.read() }
    }

    func close() {
        task.cancel(with: .normalClosure, reason: nil)
        reader?.cancel()
    }

    func subscribe(_ sessionID: String) async throws {
        var command = GameSessionCommand()
        command.subscribe = sessionID
        try await send(command)
    }

    func move(ply: UInt32, cell: UInt32, revision: UInt32?) async throws {
        var move = Axon_Game_V1_TicTacToeMove()
        move.ply = ply
        move.cell = cell
        if let revision {
            move.revision = revision
        }
        var command = GameSessionCommand()
        command.move = move
        try await send(command)
    }

    @discardableResult
    func play(cell: UInt32, atPly ply: UInt32, timeout: Duration = .seconds(20)) async throws -> GameSession {
        let matched = try await session(atPly: ply, timeout: timeout)
        let position = latest.flatMap { $0.ply == ply ? $0 : nil } ?? matched
        try await move(ply: ply, cell: cell, revision: position.hasRevision ? position.revision : nil)
        return position
    }

    func checkersMove(_ text: String, ply: UInt32, revision: UInt32?) async throws {
        var move = Axon_Game_V1_CheckersMove()
        move.ply = ply
        move.move = text
        if let revision {
            move.revision = revision
        }
        var command = GameSessionCommand()
        command.checkersMove = move
        try await send(command)
    }

    @discardableResult
    func play(checkers move: String, atPly ply: UInt32, timeout: Duration = .seconds(20)) async throws -> GameSession {
        let matched = try await session(atPly: ply, timeout: timeout)
        let position = latest.flatMap { $0.ply == ply ? $0 : nil } ?? matched
        try await checkersMove(move, ply: ply, revision: position.hasRevision ? position.revision : nil)
        return position
    }

    func session(atPly ply: UInt32, timeout: Duration = .seconds(20)) async throws -> GameSession {
        try await nextSession("ply \(ply)", timeout: timeout) { $0.ply == ply }
    }

    func nextSession(
        _ waiting: String,
        timeout: Duration = .seconds(20),
        where matches: @Sendable (GameSession) -> Bool
    ) async throws -> GameSession {
        let deadline = ContinuousClock.now.advanced(by: timeout)
        while true {
            while !pending.isEmpty {
                switch pending.removeFirst().update {
                case .session(let session) where matches(session):
                    return session
                case .error(let error):
                    throw OpponentError.sessionError(error, waiting)
                default:
                    continue
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

    private func send(_ command: GameSessionCommand) async throws {
        try await task.send(.string(command.jsonString()))
    }

    private func read() async {
        while !Task.isCancelled {
            do {
                let event = try Self.decode(try await task.receive())
                if case .session(let session) = event.update {
                    latest = session
                }
                pending.append(event)
            } catch {
                closedBy = error
                break
            }
        }
        closed = true
    }

    private static func decode(_ message: URLSessionWebSocketTask.Message) throws -> GameSessionEvent {
        switch message {
        case .string(let text):
            return try GameSessionEvent(jsonString: text, options: WireJSON.decoding)
        case .data(let data):
            return try GameSessionEvent(jsonUTF8Data: data, options: WireJSON.decoding)
        @unknown default:
            throw OpponentError.socketClosed("a known frame type", nil)
        }
    }
}
