import CryptoKit
import Foundation

enum OAuthProvider: String, CaseIterable, Identifiable, Sendable {
    case github

    var id: String { rawValue }

    var title: String {
        switch self {
        case .github: "GitHub"
        }
    }

    var proto: Axon_Auth_V1_OauthProvider {
        switch self {
        case .github: .github
        }
    }

    init?(proto: Axon_Auth_V1_OauthProvider) {
        guard let match = Self.allCases.first(where: { $0.proto == proto }) else { return nil }
        self = match
    }
}

struct OAuthPKCE: Equatable, Sendable {
    let verifier: String
    let challenge: String

    init(verifier: String) {
        self.verifier = verifier
        challenge = Data(SHA256.hash(data: Data(verifier.utf8))).base64URLEncoded
    }

    static func random() -> OAuthPKCE {
        var generator = SystemRandomNumberGenerator()
        let bytes = (0..<32).map { _ in UInt8.random(in: .min ... .max, using: &generator) }
        return OAuthPKCE(verifier: Data(bytes).base64URLEncoded)
    }
}

struct OAuthStart: Equatable, Sendable {
    let authorizationURL: URL
    let state: String
}

struct OAuthCallback: Equatable, Sendable {
    static let scheme = "axon"

    let code: String
    let state: String

    init(url: URL, provider: OAuthProvider) throws(APIError) {
        guard url.scheme == Self.scheme, url.host() == "oauth", url.path() == "/\(provider.rawValue)",
              let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems
        else {
            throw APIError(.invalidArgument, "\(provider.title) returned an unexpected address.")
        }
        let value = { (name: String) in items.first { $0.name == name }?.value ?? "" }
        switch value("error") {
        case "":
            break
        case "access_denied":
            throw APIError(.cancelled)
        default:
            throw APIError(.failedPrecondition, "\(provider.title) did not complete the sign-in.")
        }
        code = value("code")
        state = value("state")
        guard !code.isEmpty, !state.isEmpty else {
            throw APIError(.invalidArgument, "\(provider.title) returned no sign-in code.")
        }
    }
}

extension Data {
    var base64URLEncoded: String {
        base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}
