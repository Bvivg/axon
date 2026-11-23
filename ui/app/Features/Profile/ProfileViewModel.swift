import Foundation
import Observation

@MainActor
@Observable
final class ProfileViewModel {
    private(set) var user: User
    private(set) var linked: Set<OAuthProvider> = []
    private(set) var linkedLoaded = false
    private(set) var sessions: [SignInSession] = []
    private(set) var sessionsLoaded = false
    private(set) var sessionsError: String?
    private(set) var linking: OAuthProvider?
    private(set) var linkError: String?
    private(set) var ending: String?
    private(set) var ended: String?
    private(set) var endError: String?
    private(set) var saving = false
    private(set) var saved = false
    private(set) var saveError: String?
    private(set) var usernameError: String?
    private(set) var uploadingPhoto = false
    private(set) var photoError: String?
    var email = ""
    var username = ""
    var firstName = ""
    var lastName = ""
    var nickname = ""
    @ObservationIgnored private let services: Services
    @ObservationIgnored private let onUser: (User) -> Void

    init(services: Services, user: User, onUser: @escaping (User) -> Void = { _ in }) {
        self.services = services
        self.user = user
        self.onUser = onUser
        resetForm()
    }

    var current: [SignInSession] { sessions.filter(\.current) }
    var others: [SignInSession] { sessions.filter { !$0.current } }

    var emailChanged: Bool { trimmed(email) != user.email }

    var changes: ProfileChanges {
        var changes = ProfileChanges()
        if emailChanged { changes.email = trimmed(email) }
        if trimmed(username).lowercased() != (user.hasUsername ? user.username : "") { changes.username = trimmed(username) }
        if trimmed(firstName) != (user.hasFirstName ? user.firstName : "") { changes.firstName = trimmed(firstName) }
        if trimmed(lastName) != (user.hasLastName ? user.lastName : "") { changes.lastName = trimmed(lastName) }
        if trimmed(nickname) != (user.hasNickname ? user.nickname : "") { changes.nickname = trimmed(nickname) }
        return changes
    }

    var canSave: Bool {
        !saving && !changes.isEmpty && trimmed(email).contains("@") && !trimmed(username).isEmpty
    }

    func resetForm() {
        email = user.email
        username = user.hasUsername ? user.username : ""
        firstName = user.hasFirstName ? user.firstName : ""
        lastName = user.hasLastName ? user.lastName : ""
        nickname = user.hasNickname ? user.nickname : ""
    }

    func loadAccount() async {
        do {
            let account = try await services.auth.account()
            linked = account.linked
            linkedLoaded = true
            adopt(account.user)
        } catch where !linkedLoaded {
            linkError = Self.message(error)
        } catch {}
    }

    func loadSessions() async {
        do {
            sessions = try await services.auth.sessions().sorted { $0.lastUsedAt.seconds > $1.lastUsedAt.seconds }
            sessionsError = nil
        } catch {
            sessionsError = Self.message(error)
        }
        sessionsLoaded = true
    }

    func save() async {
        guard canSave else { return }
        saving = true
        saved = false
        saveError = nil
        usernameError = nil
        defer { saving = false }
        do {
            let next = try await services.auth.updateProfile(changes)
            adopt(next)
            resetForm()
            saved = true
        } catch {
            if error.message.localizedCaseInsensitiveContains("username") {
                usernameError = Self.message(error)
            } else {
                saveError = Self.message(error)
            }
        }
    }

    func uploadPhoto(_ picked: Data) async {
        guard !uploadingPhoto else { return }
        uploadingPhoto = true
        photoError = nil
        defer { uploadingPhoto = false }
        guard let jpeg = await Task.detached(operation: { AvatarImage.jpeg(from: picked) }).value else {
            photoError = "That file is not an image we can use."
            return
        }
        guard jpeg.count <= AvatarImage.maxBytes else {
            photoError = "That image is larger than 5 MB."
            return
        }
        do {
            let uploaded = try await services.media.uploadAvatar(jpeg, type: "image/jpeg")
            var next = user
            next.avatarURL = uploaded.url
            next.avatarUrls.small = uploaded.sizes.small
            next.avatarUrls.medium = uploaded.sizes.medium
            next.avatarUrls.large = uploaded.sizes.large
            next.avatarUrls.original = uploaded.sizes.original
            adopt(next)
        } catch {
            photoError = error.message.isEmpty ? "Could not upload that image." : error.message
        }
    }

    func end(_ session: SignInSession) async {
        guard ending == nil else { return }
        ending = session.id
        ended = nil
        endError = nil
        defer { ending = nil }
        do {
            try await services.auth.revokeSession(id: session.id)
            sessions.removeAll { $0.id == session.id }
            ended = "Session ended on \(session.device)."
        } catch {
            endError = Self.message(error)
        }
    }

    func connect(_ provider: OAuthProvider, present: (URL) async throws -> URL?) async {
        guard linking == nil else { return }
        linking = provider
        linkError = nil
        defer { linking = nil }
        let pkce = OAuthPKCE.random()
        let start: OAuthStart
        do {
            start = try await services.auth.startLink(provider, challenge: pkce.challenge)
        } catch {
            linkError = Self.message(error)
            return
        }
        let returned: URL?
        do {
            returned = try await present(start.authorizationURL)
        } catch {
            linkError = "Couldn't open the \(provider.title) page."
            return
        }
        guard let returned else { return }
        do {
            let callback = try OAuthCallback(url: returned, provider: provider)
            guard callback.state == start.state else {
                linkError = "The \(provider.title) request expired. Try again."
                return
            }
            let next = try await services.auth.completeLink(provider, code: callback.code, state: callback.state, verifier: pkce.verifier)
            linked.insert(provider)
            adopt(next)
        } catch {
            if error.code != .cancelled {
                linkError = Self.message(error)
            }
        }
    }

    private func adopt(_ next: User) {
        guard next.id == user.id else { return }
        user = next
        onUser(next)
    }

    private func trimmed(_ text: String) -> String {
        text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    static func message(_ error: APIError) -> String {
        switch error.code {
        case .unavailable: "The server is unreachable."
        case .unauthenticated: "Your session expired. Sign in again."
        case .resourceExhausted: "Too many attempts. Try again in a minute."
        default: error.message.isEmpty ? "Something went wrong. Try again." : error.message
        }
    }
}
