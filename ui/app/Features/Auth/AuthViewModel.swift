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

    var canSubmit: Bool { validationMessage == nil && !isSubmitting }

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

    static func message(for error: APIError) -> String {
        switch error.code {
        case .unauthenticated: "Wrong email or password."
        case .alreadyExists: error.message.isEmpty ? "This account already exists." : error.message
        case .invalidArgument: error.message.isEmpty ? "Check the fields and try again." : error.message
        case .resourceExhausted: "Too many attempts. Try again in a minute."
        case .unavailable: "The server is unreachable. Check the gateway address in Settings."
        default: error.message.isEmpty ? "Something went wrong. Try again." : error.message
        }
    }
}
