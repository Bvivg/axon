import SwiftUI

struct RootView: View {
    @Bindable var app: AppModel
    @State private var showsSettings = false

    var body: some View {
        Group {
            switch app.phase {
            case .launching:
                ProgressView()
                    .controlSize(.large)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            case .signedOut:
                AuthView(services: app.services, onSignedIn: app.signedIn, onSettings: { showsSettings = true })
            case .signedIn(let user):
                AdaptiveNavigation(path: $app.path) {
                    LobbyView(
                        services: app.services,
                        user: user,
                        onOpen: { app.path.open($0) },
                        onSettings: { showsSettings = true },
                        onSignOut: { Task { await app.signOut() } }
                    )
                } placeholder: {
                    ContentUnavailableView("Pick a game", systemImage: "gamecontroller")
                } destination: { route in
                    switch route {
                    case .game(let id):
                        GameView(sessionID: id, services: app.services, user: user, onReplay: { app.path.append(.replay(id)) })
                    case .replay(let id):
                        ReplayView(sessionID: id, services: app.services)
                    }
                }
                .id(user.id)
            }
        }
        .sheet(isPresented: $showsSettings) {
            SettingsView(current: app.settings.address) { address in
                await app.updateGateway(address)
            }
        }
        .task { await app.start() }
    }
}
