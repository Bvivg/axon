import SwiftUI

struct HomeView: View {
    @Bindable var app: AppModel
    let user: User
    let onSettings: () -> Void
    @State private var lobby: LobbyViewModel
    @State private var chats: ChatsViewModel
    @State private var calls: CallsViewModel
    @State private var engine: CallEngine
    @State private var profile: ProfileViewModel
    @State private var palette = false
    @State private var activities = CallActivities()
    @Environment(\.scenePhase) private var scenePhase

    init(app: AppModel, user: User, onSettings: @escaping () -> Void) {
        self.app = app
        self.user = user
        self.onSettings = onSettings
        _lobby = State(initialValue: LobbyViewModel(services: app.services, me: user.id) { [weak app] route in
            app?.open(route)
        })
        let alerts = LocalChatAlerts()
        alerts.onOpen = { [weak app] room in app?.openChat(room) }
        let chats = ChatsViewModel(services: app.services, me: user.id, alerts: alerts) { [weak app] route in
            app?.open(route)
        }
        chats.onRevoked = { [weak app] in
            Task { await app?.signOut() }
        }
        _chats = State(initialValue: chats)
        let calls = CallsViewModel(services: app.services, me: user.id)
        _calls = State(initialValue: calls)
        let system = Self.callSystem()
        let engine = CallEngine(
            api: app.services.calls,
            me: user.id,
            factory: LiveCallMediaFactory(manualAudio: system != nil),
            permission: SystemCapturePermission(),
            system: system,
            sfuURL: { [settings = app.settings] path in settings.address.sfuURL(path) }
        )
        engine.send = { [weak chats] command in await chats?.send(command) ?? false }
        engine.names = { [weak chats, weak calls] id in
            chats?.people[id]?.shownName ?? calls?.people[id]?.shownName ?? "Axon user"
        }
        engine.profiles = { [weak chats] ids in await chats?.ensureProfiles(ids) }
        engine.historyChanged = { [weak calls] in calls?.scheduleRefresh() }
        chats.listen(engine)
        _engine = State(initialValue: engine)
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
                stack(.calls) { CallsView(model: calls, hub: chats) }
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
        .task { await chats.run() }
        .task { await engine.activate() }
        .modifier(CallPresenter(engine: engine, hub: chats))
        .sheet(isPresented: $palette) {
            CommandPalette(items: PaletteCatalog.items(chats: chats)) { link in app.handle(link) }
        }
        .onChange(of: scenePhase, initial: true) { _, phase in
            chats.sceneActive = phase == .active
            chats.sceneVisible = phase != .background
            if phase == .active { chats.resume() }
        }
        .onChange(of: app.pendingLink, initial: true) { _, link in
            guard let link else { return }
            app.pendingLink = nil
            follow(link)
        }
        .onChange(of: spotlight) { _, entries in Spotlight.index(entries) }
        .onChange(of: engine.activitySnapshot, initial: true) { _, snapshot in activities.show(snapshot) }
        .onDisappear {
            lobby.stop()
            activities.end()
        }
        .environment(\.media, app.services.media)
    }

    private var unread: Int { chats.unread }

    private var spotlight: [SpotlightEntry] {
        Spotlight.games(lobby.mine, me: user.id) { lobby.opponentName(in: $0) }
            + Spotlight.rooms(chats.rooms) { chats.title(of: $0) }
    }

    private func follow(_ link: AppLink) {
        switch link {
        case .open(let tab, let routes):
            if routes.isEmpty {
                app.tab = tab
            } else {
                app.follow(tab, routes)
            }
        case .quickMatch(let game):
            app.follow(.games, [.lobby(game)])
            Task { await lobby.quickMatch(game) }
        case .newChat:
            app.follow(.chats, [])
            chats.composing = .direct
        case .palette:
            palette = true
        }
    }

    private static func callSystem() -> (any CallSystem)? {
        #if os(iOS)
        CallKitSystem()
        #else
        nil
        #endif
    }

    private func stack(_ tab: AppTab, @ViewBuilder root: () -> some View) -> some View {
        NavigationStack(path: Binding(get: { app.paths[tab] ?? [] }, set: { app.paths[tab] = $0 })) {
            root()
                .navigationDestination(for: Route.self) { route in
                    switch route {
                    case .lobby, .game, .replay, .practice, .tournaments, .tournament:
                        gameDestination(route)
                    default:
                        socialDestination(route, in: tab)
                    }
                }
        }
    }

    @ViewBuilder
    private func gameDestination(_ route: Route) -> some View {
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
        case .practice(let game):
            PracticeScreen(game: game)
        case .tournaments:
            TournamentsView(api: app.services.tournaments) { route in app.open(route) }
        case .tournament(let id):
            TournamentView(id: id, services: app.services, user: user)
                .id(id)
        default:
            EmptyView()
        }
    }

    @ViewBuilder
    private func socialDestination(_ route: Route, in tab: AppTab) -> some View {
        switch route {
        case .profile(let page):
            ProfilePageView(page: page, model: profile) {
                Task { await app.signOut() }
            }
        case .room(let id):
            ConversationView(target: .room(id), hub: chats)
                .id(id)
        case .person(let id):
            ConversationView(target: .person(id), hub: chats)
                .id(id)
        case .roomInfo(let id):
            RoomInfoView(roomID: id, hub: chats) {
                app.paths[tab] = []
            }
        case .roomMedia(let id):
            RoomMediaView(roomID: id, hub: chats)
        case .call(let id):
            CallDetailView(callID: id, model: calls, hub: chats)
        default:
            EmptyView()
        }
    }
}
