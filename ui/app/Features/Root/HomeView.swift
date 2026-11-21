import SwiftUI

struct HomeView: View {
    @Bindable var app: AppModel
    let user: User
    let onSettings: () -> Void
    @State private var lobby: LobbyViewModel
    @State private var chats: ChatsViewModel
    @State private var calls: CallsViewModel
    @State private var profile: ProfileViewModel

    init(app: AppModel, user: User, onSettings: @escaping () -> Void) {
        self.app = app
        self.user = user
        self.onSettings = onSettings
        _lobby = State(initialValue: LobbyViewModel(services: app.services, me: user.id) { [weak app] route in
            app?.open(route)
        })
        _chats = State(initialValue: ChatsViewModel(services: app.services, me: user.id))
        _calls = State(initialValue: CallsViewModel(services: app.services, me: user.id))
        _profile = State(initialValue: ProfileViewModel(services: app.services, user: user) { [weak app] next in
            app?.replaceUser(next)
        })
    }

    var body: some View {
        TabView(selection: $app.tab) {
            Tab("Games", systemImage: "gamecontroller", value: AppTab.games) {
                stack(.games) { GamesCatalogView(lobby: lobby) }
            }
            .accessibilityIdentifier(AccessibilityID.Home.tab(AppTab.games.rawValue))
            Tab("Calls", systemImage: "phone", value: AppTab.calls) {
                stack(.calls) { CallsView(model: calls) }
            }
            .accessibilityIdentifier(AccessibilityID.Home.tab(AppTab.calls.rawValue))
            Tab("Chats", systemImage: "message", value: AppTab.chats) {
                stack(.chats) { ChatsView(model: chats) }
            }
            .badge(unread)
            .accessibilityIdentifier(AccessibilityID.Home.tab(AppTab.chats.rawValue))
            Tab("Profile", systemImage: "person", value: AppTab.profile) {
                stack(.profile) {
                    ProfileView(model: profile, gateway: app.settings.address, onSettings: onSettings)
                }
            }
            .accessibilityIdentifier(AccessibilityID.Home.tab(AppTab.profile.rawValue))
        }
        .tabViewStyle(.sidebarAdaptable)
        #if os(iOS)
        .tabBarMinimizeBehavior(.onScrollDown)
        #endif
        .task { await lobby.run() }
        .task { await profile.loadAccount() }
        .task { await chats.refresh() }
        .onDisappear { lobby.stop() }
    }

    private var unread: Int {
        chats.rooms.reduce(0) { $0 + Int($1.unreadCount) }
    }

    private func stack(_ tab: AppTab, @ViewBuilder root: () -> some View) -> some View {
        NavigationStack(path: Binding(get: { app.paths[tab] ?? [] }, set: { app.paths[tab] = $0 })) {
            root()
                .navigationDestination(for: Route.self) { route in
                    switch route {
                    case .lobby(let game):
                        GameLobbyView(game: game, lobby: lobby)
                    case .game(let id):
                        GameView(sessionID: id, services: app.services, user: user) {
                            app.open(.replay(id))
                        }
                        .id(id)
                    case .replay(let id):
                        ReplayView(sessionID: id, services: app.services)
                            .id(id)
                    case .profile(let page):
                        ProfilePageView(page: page, model: profile) {
                            Task { await app.signOut() }
                        }
                    }
                }
        }
    }
}
