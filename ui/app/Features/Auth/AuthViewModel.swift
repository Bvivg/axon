import Foundation
import Observation

@MainActor
@Observable
final class AuthViewModel {
    enum Mode: String, CaseIterable, Identifiable {
        case login = "Sign in"
        case register = "Create account"

        var id: String { rawValue }
    }

    var mode: Mode = .login
    var email = ""
    var username = ""
    var password = ""
    private(set) var isSubmitting = false
    private(set) var signingInWith: OAuthProvider?
    private(set) var error: String?
    @ObservationIgnored private let auth: any AuthAPI

    init(auth: any AuthAPI) {
        self.auth = auth
    }

    var trimmedEmail: String { email.trimmingCharacters(in: .whitespacesAndNewlines) }
    var trimmedUsername: String { username.trimmingCharacters(in: .whitespacesAndNewlines) }

    var validationMessage: String? {
        if trimmedEmail.isEmpty || password.isEmpty { return "" }
        if !trimmedEmail.contains("@") { return "Enter a valid email." }
        if mode == .register {
            if trimmedUsername.count < 3 { return "Username needs at least 3 characters." }
            if password.count < 8 { return "Password needs at least 8 characters." }
        }
        return nil
    }

    var isBusy: Bool { isSubmitting || signingInWith != nil }

    var canSubmit: Bool { validationMessage == nil && !isBusy }

    func submit() async -> User? {
        guard canSubmit else { return nil }
        isSubmitting = true
        error = nil
        defer { isSubmitting = false }
        do {
            switch mode {
            case .login:
                return try await auth.login(email: trimmedEmail, password: password)
            case .register:
                return try await auth.register(email: trimmedEmail, username: trimmedUsername, password: password)
            }
        } catch {
            self.error = Self.message(for: error)
            return nil
        }
    }

    func signIn(with provider: OAuthProvider, present: (URL) async throws -> URL?) async -> User? {
        guard !isBusy else { return nil }
        signingInWith = provider
        error = nil
        defer { signingInWith = nil }
        let pkce = OAuthPKCE.random()
        let start: OAuthStart
        do {
            start = try await auth.startOAuth(provider, challenge: pkce.challenge)
        } catch {
            self.error = Self.message(for: error)
            return nil
        }
        let returned: URL?
        do {
            returned = try await present(start.authorizationURL)
        } catch {
            self.error = "Couldn't open the \(provider.title) sign-in page."
            return nil
        }
        guard let returned else { return nil }
        do {
            let callback = try OAuthCallback(url: returned, provider: provider)
            guard callback.state == start.state else {
                self.error = "The \(provider.title) sign-in expired. Try again."
                return nil
            }
            return try await auth.completeOAuth(provider, code: callback.code, state: callback.state, verifier: pkce.verifier)
        } catch {
            if error.code != .cancelled {
                self.error = Self.message(for: error)
            }
            return nil
        }
    }

    static func message(for error: APIError) -> String {
        switch error.code {
        case .unauthenticated: "Wrong email or password."
        case .alreadyExists: error.message.isEmpty ? "This account already exists." : error.message
        case .invalidArgument: error.message.isEmpty ? "Check the fields and try again." : error.message
        case .resourceExhausted: "Too many attempts. Try again in a minute."
        case .unavailable: "The server is unreachable. Check the gateway address in Settings."
        case .failedPrecondition: error.message.isEmpty ? "The provider could not sign you in." : error.message
        default: error.message.isEmpty ? "Something went wrong. Try again." : error.message
        }
    }
}
