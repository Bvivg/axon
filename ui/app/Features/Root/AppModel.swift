import Foundation
import Observation

enum Route: Hashable, Sendable {
    case game(String)
    case replay(String)
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
    var path: [Route] = []
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
        path = []
        phase = .signedIn(user)
    }

    func signOut() async {
        await services.auth.logout()
        path = []
        phase = .signedOut
    }

    func updateGateway(_ address: GatewayAddress) async {
        guard address != settings.address else { return }
        if user != nil {
            await services.auth.logout()
        }
        await vault.clear()
        settings.update(address)
        services = build(settings, vault)
        path = []
        phase = .signedOut
        watch()
    }

    private func watch() {
        watcher?.cancel()
        let signals = services.auth.signedOut
        watcher = Task { [weak self] in
            for await _ in signals {
                guard let self else { return }
                self.path = []
                self.phase = .signedOut
            }
        }
    }
}
