import Connect
import Foundation
import SwiftProtobuf

extension APIError.Code {
    init(_ code: Connect.Code) {
        switch code {
        case .unauthenticated: self = .unauthenticated
        case .invalidArgument, .outOfRange: self = .invalidArgument
        case .alreadyExists: self = .alreadyExists
        case .notFound: self = .notFound
        case .permissionDenied: self = .permissionDenied
        case .failedPrecondition, .aborted: self = .failedPrecondition
        case .resourceExhausted: self = .resourceExhausted
        case .unavailable, .deadlineExceeded: self = .unavailable
        case .canceled: self = .cancelled
        case .ok, .unknown, .unimplemented, .internalError, .dataLoss: self = .unknown
        }
    }
}

extension APIError {
    init(_ error: ConnectError) {
        self.init(Code(error.code), error.message ?? "")
    }
}

extension ResponseMessage {
    func unwrapped() throws(APIError) -> Output {
        switch result {
        case .success(let message): return message
        case .failure(let error): throw APIError(error)
        }
    }
}

final class DeviceHeaderInterceptor: UnaryInterceptor {
    let deviceID: String

    init(deviceID: String) {
        self.deviceID = deviceID
    }

    func handleUnaryRequest<Message: ProtobufMessage>(
        _ request: HTTPRequest<Message>,
        proceed: @escaping @Sendable (Result<HTTPRequest<Message>, ConnectError>) -> Void
    ) {
        var headers = request.headers
        headers[ConnectClient.deviceHeader] = [deviceID]
        proceed(.success(HTTPRequest(
            url: request.url,
            headers: headers,
            message: request.message,
            method: request.method,
            trailers: request.trailers,
            idempotencyLevel: request.idempotencyLevel
        )))
    }
}

struct ConnectClient: Sendable {
    static let deviceHeader = "X-Axon-Device"
    static let requestTimeout: TimeInterval = 20

    let vault: TokenVault

    static func transport(address: GatewayAddress, deviceID: String) -> ProtocolClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.httpShouldSetCookies = false
        configuration.httpCookieAcceptPolicy = .never
        configuration.httpCookieStorage = nil
        configuration.urlCache = nil
        return ProtocolClient(
            httpClient: URLSessionHTTPClient(configuration: configuration),
            config: ProtocolClientConfig(
                host: address.host,
                networkProtocol: .connect,
                codec: ProtoCodec(),
                timeout: requestTimeout,
                interceptors: [InterceptorFactory { _ in DeviceHeaderInterceptor(deviceID: deviceID) }]
            )
        )
    }

    static func bearer(_ token: String) -> Headers {
        ["Authorization": ["Bearer \(token)"]]
    }

    func call<Output: ProtobufMessage>(
        _ rpc: (Headers) async -> ResponseMessage<Output>
    ) async throws(APIError) -> Output {
        var token = await vault.validAccessToken()
        if token == nil {
            token = await vault.accessToken
        }
        guard let token else { throw APIError(.unauthenticated, "no session") }
        let first = await rpc(Self.bearer(token))
        guard case .failure(let error) = first.result, error.code == .unauthenticated else {
            return try first.unwrapped()
        }
        guard let renewed = await vault.refresh(rejected: token) else {
            if await vault.hasSession {
                throw APIError(.unavailable, "could not renew the session")
            }
            throw APIError(error)
        }
        return try await rpc(Self.bearer(renewed)).unwrapped()
    }
}

struct TokenRefresher: Sendable {
    static let rejectingCodes: Set<Connect.Code> = [.unauthenticated, .permissionDenied, .invalidArgument]

    let client: any Axon_Auth_V1_AuthServiceClientInterface

    func refresh(_ refreshToken: String) async -> RefreshOutcome {
        var request = Axon_Auth_V1_RefreshTokenRequest()
        request.refreshToken = refreshToken
        let response = await client.refreshToken(request: request, headers: [:])
        switch response.result {
        case .success(let message):
            guard let tokens = AuthTokens(message.tokens, previousRefreshToken: refreshToken) else {
                return .unavailable
            }
            return .rotated(tokens)
        case .failure(let error):
            return Self.rejectingCodes.contains(error.code) ? .rejected : .unavailable
        }
    }
}
