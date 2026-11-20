import Connect
import Foundation
import SwiftProtobuf
import Synchronization
import Testing
@testable import Axon

@Suite(.timeLimit(.minutes(1)))
struct ConnectClientTests {
    let client = ScriptedGameClient()

    func api(_ vault: TokenVault) -> LiveGameAPI {
        LiveGameAPI(client: client, connect: ConnectClient(vault: vault))
    }

    typealias Fetched = Result<Axon_Game_V1_GetSessionResponse, ConnectError>

    func serveSession(acceptedTokens: Set<String>) {
        client.script.on("GetSession") { (authorization: String?) -> Fetched in
            guard let authorization, acceptedTokens.contains(String(authorization.dropFirst("Bearer ".count))) else {
                return .failure(Fixtures.denied(.unauthenticated, "access token expired"))
            }
            var response = Axon_Game_V1_GetSessionResponse()
            response.session = Fixtures.session("s-1")
            return .success(response)
        }
    }

    @Test
    func sendsTheBearerTokenFromTheVault() async throws {
        serveSession(acceptedTokens: ["a-1"])
        let session = try await api(Fixtures.vault(Fixtures.tokens("a-1"))).session(id: "s-1")
        #expect(session.id == "s-1")
        #expect(client.script.recorded.map(\.authorization) == ["Bearer a-1"])
        #expect((client.script.recorded.first?.request as? Axon_Game_V1_GetSessionRequest)?.sessionID == "s-1")
    }

    @Test
    func refreshesAnExpiringTokenBeforeTheCall() async throws {
        serveSession(acceptedTokens: ["a-2"])
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2")))
        let vault = Fixtures.vault(Fixtures.tokens("a-1", expiresIn: 10), refresher: refresher)
        _ = try await api(vault).session(id: "s-1")
        #expect(client.script.recorded.map(\.authorization) == ["Bearer a-2"])
        #expect(refresher.calls == ["refresh-a-1"])
    }

    @Test
    func unauthenticatedRefreshesOnceAndRetriesOnce() async throws {
        serveSession(acceptedTokens: ["a-2"])
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2")))
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        let session = try await api(vault).session(id: "s-1")
        #expect(session.id == "s-1")
        #expect(client.script.recorded.map(\.authorization) == ["Bearer a-1", "Bearer a-2"])
        #expect(refresher.calls == ["refresh-a-1"])
        #expect(await vault.accessToken == "a-2")
    }

    @Test
    func surfacesUnauthenticatedAfterTheSingleRetry() async {
        serveSession(acceptedTokens: [])
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2")), .rotated(Fixtures.tokens("a-3")))
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        await #expect(throws: APIError(.unauthenticated, "access token expired")) {
            try await api(vault).session(id: "s-1")
        }
        #expect(client.script.recorded.map(\.authorization) == ["Bearer a-1", "Bearer a-2"])
        #expect(refresher.calls.count == 1)
    }

    @Test
    func rejectedRefreshSignsOut() async {
        serveSession(acceptedTokens: [])
        let refresher = CountingRefresher(.rejected)
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        await #expect(throws: APIError(.unauthenticated, "access token expired")) {
            try await api(vault).session(id: "s-1")
        }
        #expect(client.script.recorded.count == 1)
        #expect(await vault.hasSession == false)
        var signOuts = vault.signedOut.makeAsyncIterator()
        #expect(await signOuts.next() != nil)
    }

    @Test
    func unavailableRefreshKeepsTheSession() async {
        serveSession(acceptedTokens: [])
        let refresher = CountingRefresher(.unavailable)
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        await #expect(throws: APIError(.unavailable, "could not renew the session")) {
            try await api(vault).session(id: "s-1")
        }
        #expect(await vault.accessToken == "a-1")
    }

    @Test
    func failsFastWithoutASession() async {
        serveSession(acceptedTokens: ["a-1"])
        await #expect(throws: APIError(.unauthenticated, "no session")) {
            try await api(Fixtures.vault(nil)).session(id: "s-1")
        }
        #expect(client.script.recorded.isEmpty)
    }

    @Test
    func concurrentRejectionsShareOneRefresh() async throws {
        serveSession(acceptedTokens: ["a-2"])
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2")), .rotated(Fixtures.tokens("a-3")))
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        let api = api(vault)
        let ids = try await withThrowingTaskGroup(of: String.self) { group in
            for _ in 0..<8 {
                group.addTask { try await api.session(id: "s-1").id }
            }
            return try await group.reduce(into: [String]()) { $0.append($1) }
        }
        #expect(ids == Array(repeating: "s-1", count: 8))
        #expect(refresher.calls == ["refresh-a-1"])
        #expect(await vault.accessToken == "a-2")
    }

    @Test
    func vaultRefreshIsSingleFlight() async {
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2")), .rotated(Fixtures.tokens("a-3")))
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        let tokens = await withTaskGroup(of: String?.self) { group in
            for _ in 0..<16 {
                group.addTask { await vault.refresh(rejected: "a-1") }
            }
            return await group.reduce(into: [String?]()) { $0.append($1) }
        }
        #expect(tokens == Array(repeating: "a-2", count: 16))
        #expect(refresher.calls.count == 1)
    }

    @Test
    func signingOutDuringARefreshDiscardsTheRotatedTokens() async {
        let started = AsyncStream<Void>.makeStream()
        let gate = AsyncStream<Void>.makeStream()
        let vault = Fixtures.vault(Fixtures.tokens("a-1"))
        vault.setRefresher { _ in
            started.continuation.yield()
            for await _ in gate.stream { break }
            return .rotated(Fixtures.tokens("a-2"))
        }
        let refresh = Task { await vault.refresh(rejected: "a-1") }
        for await _ in started.stream { break }
        await vault.clear()
        gate.continuation.yield()
        #expect(await refresh.value == nil)
        #expect(await vault.hasSession == false)
    }

    static let codeMapping: [(Connect.Code, APIError.Code)] = [
        (.unauthenticated, .unauthenticated),
        (.invalidArgument, .invalidArgument),
        (.outOfRange, .invalidArgument),
        (.alreadyExists, .alreadyExists),
        (.notFound, .notFound),
        (.permissionDenied, .permissionDenied),
        (.failedPrecondition, .failedPrecondition),
        (.aborted, .failedPrecondition),
        (.resourceExhausted, .resourceExhausted),
        (.unavailable, .unavailable),
        (.deadlineExceeded, .unavailable),
        (.canceled, .cancelled),
        (.unknown, .unknown),
        (.internalError, .unknown),
        (.unimplemented, .unknown),
        (.dataLoss, .unknown),
    ]

    @Test(arguments: codeMapping)
    func mapsConnectCodes(code: Connect.Code, expected: APIError.Code) {
        #expect(APIError.Code(code) == expected)
        #expect(APIError(ConnectError(code: code, message: "server said so")) == APIError(expected, "server said so"))
    }

    @Test
    func keepsTheServerMessageOnFailures() async {
        client.script.on("JoinSession") { (_: String?) -> Result<Axon_Game_V1_JoinSessionResponse, ConnectError> in
            .failure(Fixtures.denied(.failedPrecondition, "session is full"))
        }
        await #expect(throws: APIError(.failedPrecondition, "session is full")) {
            try await api(Fixtures.vault(Fixtures.tokens("a-1"))).joinSession(id: "s-1")
        }
        #expect(APIError(ConnectError(code: .unavailable, message: nil)) == APIError(.unavailable, ""))
    }

    @Test
    func deviceHeaderInterceptorTagsEveryRequest() {
        let interceptor = DeviceHeaderInterceptor(deviceID: "3f1d2c4b-0000-4000-8000-000000000001")
        let request = HTTPRequest(
            url: URL(string: "http://localhost:18080/axon.auth.v1.AuthService/Login")!,
            headers: ["Content-Type": ["application/proto"]],
            message: Axon_Auth_V1_LoginRequest(),
            method: .post,
            trailers: nil,
            idempotencyLevel: .unknown
        )
        let sink = HeaderSink()
        interceptor.handleUnaryRequest(request) { result in
            sink.headers.withLock { $0 = try? result.get().headers }
        }
        let headers = sink.headers.withLock { $0 }
        #expect(headers?[ConnectClient.deviceHeader] == ["3f1d2c4b-0000-4000-8000-000000000001"])
        #expect(headers?["Content-Type"] == ["application/proto"])
        #expect(headers?["Origin"] == nil)
    }
}

@Suite
struct TokenRefresherTests {
    let client = ScriptedAuthClient()

    func respond(_ result: Result<Axon_Auth_V1_RefreshTokenResponse, ConnectError>) {
        client.script.on("RefreshToken") { (_: String?) in result }
    }

    func response(access: String, refresh: String) -> Axon_Auth_V1_RefreshTokenResponse {
        var response = Axon_Auth_V1_RefreshTokenResponse()
        response.tokens = Fixtures.pair(access, refresh: refresh)
        return response
    }

    @Test
    func rotatesTokensWithoutABearer() async throws {
        respond(.success(response(access: "a-2", refresh: "r-2")))
        let outcome = await TokenRefresher(client: client).refresh("r-1")
        guard case .rotated(let tokens) = outcome else {
            Issue.record("expected rotated tokens, got \(outcome)")
            return
        }
        #expect(tokens.accessToken == "a-2")
        #expect(tokens.refreshToken == "r-2")
        #expect(tokens.expiresAt.timeIntervalSinceNow > 800)
        let call = try #require(client.script.recorded("RefreshToken").first)
        #expect(call.authorization == nil)
        #expect((call.request as? Axon_Auth_V1_RefreshTokenRequest)?.refreshToken == "r-1")
    }

    @Test
    func keepsThePreviousRefreshTokenWhenNoneIsReturned() async {
        respond(.success(response(access: "a-2", refresh: "")))
        let outcome = await TokenRefresher(client: client).refresh("r-1")
        guard case .rotated(let tokens) = outcome else {
            Issue.record("expected rotated tokens, got \(outcome)")
            return
        }
        #expect(tokens.refreshToken == "r-1")
    }

    @Test
    func emptyTokensAreNotARejection() async {
        respond(.success(Axon_Auth_V1_RefreshTokenResponse()))
        #expect(await TokenRefresher(client: client).refresh("r-1") == .unavailable)
    }

    @Test(arguments: [Connect.Code.unauthenticated, .permissionDenied, .invalidArgument])
    func rejects(code: Connect.Code) async {
        respond(.failure(Fixtures.denied(code)))
        #expect(await TokenRefresher(client: client).refresh("r-1") == .rejected)
    }

    @Test(arguments: [
        Connect.Code.unavailable, .deadlineExceeded, .internalError, .unknown, .resourceExhausted, .canceled,
    ])
    func reportsUnavailable(code: Connect.Code) async {
        respond(.failure(Fixtures.denied(code)))
        #expect(await TokenRefresher(client: client).refresh("r-1") == .unavailable)
    }
}

final class HeaderSink: Sendable {
    let headers = Mutex<Headers?>(nil)
}
