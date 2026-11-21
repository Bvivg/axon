import SwiftUI

struct LobbyView: View {
    @State private var model: LobbyViewModel
    let user: User
    let onSettings: () -> Void
    let onSignOut: () -> Void

    init(
        services: Services,
        user: User,
        onOpen: @escaping (Route) -> Void,
        onSettings: @escaping () -> Void,
        onSignOut: @escaping () -> Void
    ) {
        _model = State(initialValue: LobbyViewModel(services: services, me: user.id, open: onOpen))
        self.user = user
        self.onSettings = onSettings
        self.onSignOut = onSignOut
    }

    var body: some View {
        List {
            if let error = model.error {
                Section {
                    Label(error, systemImage: "exclamationmark.triangle")
                        .foregroundStyle(.red)
                        .accessibilityIdentifier(AccessibilityID.Lobby.error)
                }
            }
            Section("Play") {
                ForEach(GameKind.allCases) { game in
                    GameCard(game: game, model: model)
                }
                if let game = model.matching {
                    HStack {
                        ProgressView()
                        Text("Looking for a \(game.title) opponent")
                        Spacer()
                        Button("Cancel") { Task { await model.cancelMatch() } }
                            .accessibilityIdentifier(AccessibilityID.Lobby.cancelMatch)
                    }
                }
            }
            Section("Join by ID") {
                HStack {
                    TextField("Game ID or link", text: $model.joinID)
                        .autocorrectionDisabled()
                        #if os(iOS)
                        .textInputAutocapitalization(.never)
                        #endif
                        .accessibilityIdentifier(AccessibilityID.Lobby.joinID)
                        .onSubmit { Task { await model.join() } }
                    Button("Join") { Task { await model.join() } }
                        .disabled(model.joinID.trimmingCharacters(in: .whitespaces).isEmpty || model.busy != nil)
                        .accessibilityIdentifier(AccessibilityID.Lobby.join)
                }
            }
            if !model.active.isEmpty {
                Section("Your games") {
                    ForEach(model.active, id: \.id) { session in
                        SessionRow(session: session, title: model.opponentName(in: session), me: model.me) {
                            model.openGame(session.id)
                        }
                    }
                }
            }
            if !model.live.isEmpty {
                Section("Live games") {
                    ForEach(model.live, id: \.id) { session in
                        SessionRow(session: session, title: session.playerIds.map(model.name).joined(separator: " vs "), me: model.me) {
                            model.openGame(session.id)
                        }
                    }
                }
            }
            if !model.finished.isEmpty {
                Section("Finished") {
                    ForEach(model.finished.prefix(20), id: \.id) { session in
                        SessionRow(session: session, title: model.opponentName(in: session), me: model.me) {
                            model.openReplay(session.id)
                        }
                    }
                }
            }
        }
        .accessibilityIdentifier(AccessibilityID.Lobby.root)
        .navigationTitle("Lobby")
        .refreshable { await model.refresh() }
        .overlay {
            if model.isLoading && model.mine.isEmpty && model.live.isEmpty {
                ProgressView()
            }
        }
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Text(user.hasUsername ? user.username : user.email)
                    Button("Settings", systemImage: "gearshape", action: onSettings)
                        .accessibilityIdentifier(AccessibilityID.Settings.open)
                    Button("Sign out", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive, action: onSignOut)
                        .accessibilityIdentifier(AccessibilityID.Auth.logout)
                } label: {
                    Label("Account", systemImage: "person.crop.circle")
                }
            }
        }
        .task { await model.run() }
        .onDisappear { model.stop() }
    }
}

private struct GameCard: View {
    let game: GameKind
    let model: LobbyViewModel
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif

    private var stacked: Bool {
        #if os(iOS)
        sizeClass == .compact
        #else
        false
        #endif
    }

    var body: some View {
        Group {
            if stacked {
                VStack(alignment: .leading, spacing: 10) {
                    header
                    actions
                }
            } else {
                HStack(spacing: 12) {
                    header
                    Spacer(minLength: 8)
                    actions
                }
            }
        }
        .disabled(model.busy != nil && model.busy != "match.\(game.rawValue)" && model.busy != "create.\(game.rawValue)")
        .padding(.vertical, 4)
    }

    private var header: some View {
        HStack(spacing: 12) {
            Image(systemName: game.symbol)
                .font(.title2)
                .frame(width: 36)
                .foregroundStyle(.tint)
            Text(game.title)
                .font(.headline)
                .lineLimit(1)
        }
    }

    private var actions: some View {
        HStack(spacing: 8) {
            Button {
                Task { await model.create(game) }
            } label: {
                label("New", busy: model.busy == "create.\(game.rawValue)")
            }
            .buttonStyle(.bordered)
            .accessibilityIdentifier(AccessibilityID.Lobby.create(game.rawValue))
            Button {
                Task { await model.quickMatch(game) }
            } label: {
                label("Quick match", busy: model.busy == "match.\(game.rawValue)")
            }
            .buttonStyle(.borderedProminent)
            .disabled(model.matching != nil)
            .accessibilityIdentifier(AccessibilityID.Lobby.quickMatch(game.rawValue))
        }
    }

    private func label(_ title: String, busy: Bool) -> some View {
        Group {
            if busy {
                ProgressView().controlSize(.small)
            } else {
                Text(title).lineLimit(1)
            }
        }
        .frame(maxWidth: stacked ? .infinity : nil)
    }
}

private struct SessionRow: View {
    let session: Session
    let title: String
    let me: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Image(systemName: session.kind.symbol)
                    .frame(width: 28)
                    .foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.body)
                        .lineLimit(1)
                    Text("\(session.kind.title) · \(SessionStatusText.headline(session, me: me))")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if session.hasSpectators, session.spectators > 0 {
                    Label("\(session.spectators)", systemImage: "eye")
                        .labelStyle(.titleAndIcon)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Image(systemName: "chevron.right")
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier(AccessibilityID.Lobby.session(session.id))
    }
}
