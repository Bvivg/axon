import Foundation
import Observation

enum Route: Hashable, Sendable {
    case lobby(GameKind)
    case game(String)
    case replay(String)
    case profile(ProfilePage)
    case room(String)
    case person(String)
    case roomInfo(String)
    case roomMedia(String)
}

enum ProfilePage: String, Hashable, Sendable, CaseIterable {
    case account
    case personal
    case sessions
    case notifications
    case appearance
}

enum AppTab: String, Hashable, Sendable, CaseIterable {
    case games
    case calls
    case chats
    case profile
}

@MainActor
@Observable
final class AppModel {
    enum Phase: Equatable {
        case launching
        case signedOut
        case signedIn(User)
    }

    typealias ServicesBuilder = @MainActor (GatewaySettings, TokenVault) -> Services

    private(set) var phase: Phase = .launching
    private(set) var services: Services
    var tab: AppTab = .games
    var paths: [AppTab: [Route]] = [:]
    let settings: GatewaySettings
    @ObservationIgnored private let vault: TokenVault
    @ObservationIgnored private let build: ServicesBuilder
    @ObservationIgnored private var watcher: Task<Void, Never>?

    init(settings: GatewaySettings, vault: TokenVault, build: @escaping ServicesBuilder) {
        self.settings = settings
        self.vault = vault
        self.build = build
        services = build(settings, vault)
    }

    var user: User? {
        if case .signedIn(let user) = phase { return user }
        return nil
    }

    func start() async {
        watch()
        if let user = await services.auth.restore() {
            phase = .signedIn(user)
        } else {
            phase = .signedOut
        }
    }

    func signedIn(_ user: User) {
        resetNavigation()
        phase = .signedIn(user)
    }

    func signOut() async {
        await services.auth.logout()
        resetNavigation()
        phase = .signedOut
    }

    func open(_ route: Route) {
        paths[tab, default: []].append(route)
    }

    func openChat(_ room: String) {
        tab = .chats
        if paths[.chats]?.last != .room(room) {
            paths[.chats] = [.room(room)]
        }
    }

    func replaceUser(_ user: User) {
        guard case .signedIn(let current) = phase, current.id == user.id else { return }
        phase = .signedIn(user)
    }

    private func resetNavigation() {
        tab = .games
        paths = [:]
    }

    func updateGateway(_ address: GatewayAddress) async {
        guard address != settings.address else { return }
        if user != nil {
            await services.auth.logout()
        }
        await vault.clear()
        settings.update(address)
        services = build(settings, vault)
        resetNavigation()
        phase = .signedOut
        watch()
    }

    private func watch() {
        watcher?.cancel()
        let signals = services.auth.signedOut
        watcher = Task { [weak self] in
            for await _ in signals {
                guard let self else { return }
                self.resetNavigation()
                self.phase = .signedOut
            }
        }
    }
}
