import SwiftUI

struct GameLobbyView: View {
    let game: GameKind
    @Bindable var lobby: LobbyViewModel
    @State private var startsGame = false

    private var rating: Rating? { lobby.ratings[game] }

    var body: some View {
        ScrollView {
            VStack(spacing: 16) {
                if let error = lobby.error {
                    ErrorBanner(text: error)
                        .accessibilityIdentifier(AccessibilityID.Lobby.error)
                }
                ratingCard
                let live = lobby.live(game)
                if !live.isEmpty {
                    SessionCard(title: "Live games", count: live.count) {
                        ForEach(live, id: \.id) { session in
                            SessionRow(session: session, title: session.playerIds.map(lobby.name).joined(separator: " vs "), me: lobby.me) {
                                lobby.openGame(session.id)
                            }
                        }
                    }
                }
                let mine = lobby.mine(game)
                SessionCard(title: "Your games", count: mine.count) {
                    if mine.isEmpty {
                        Text("No games yet")
                            .font(.axSubheadline)
                            .foregroundStyle(Theme.muted)
                            .padding(.vertical, 4)
                    }
                    ForEach(mine.prefix(30), id: \.id) { session in
                        SessionRow(session: session, title: lobby.opponentName(in: session), me: lobby.me) {
                            if session.isFinished {
                                lobby.openReplay(session.id)
                            } else {
                                lobby.openGame(session.id)
                            }
                        }
                    }
                }
            }
            .padding(16)
            .frame(maxWidth: 720)
            .frame(maxWidth: .infinity)
        }
        .background(Theme.background)
        .navigationTitle(game.title)
        .navigationSubtitle(game.players)
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .refreshable { await lobby.refresh() }
        .sheet(isPresented: $startsGame) {
            StartGameSheet(game: game, lobby: lobby)
        }
        .safeAreaInset(edge: .bottom) {
            if let matching = lobby.matching {
                MatchingBanner(game: matching, lobby: lobby)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .animation(.snappy, value: lobby.matching)
    }

    private var ratingCard: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Rating")
                .font(.axFootnote)
                .foregroundStyle(Theme.muted)
            Text(verbatim: "\(lobby.rating(game))")
                .font(.axMono(28, medium: true, relativeTo: .title))
                .foregroundStyle(Theme.foreground)
                .contentTransition(.numericText())
            Text(rating?.record ?? "0 rated · 0W 0L 0D")
                .font(.axFootnote)
                .foregroundStyle(Theme.muted)
            Button {
                startsGame = true
            } label: {
                Label("Start a game", systemImage: "play")
                    .font(.geist(15, .medium, relativeTo: .body))
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 6)
            }
            .buttonStyle(.borderedProminent)
            .buttonBorderShape(.roundedRectangle(radius: 10))
            .controlSize(.large)
            .disabled(lobby.matching != nil)
            .padding(.top, 12)
            .accessibilityIdentifier(AccessibilityID.Lobby.start)
        }
        .surface(padding: 16)
    }
}

struct SessionCard<Rows: View>: View {
    let title: String
    let count: Int
    @ViewBuilder let rows: () -> Rows

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text(title)
                    .font(.geist(15, .semibold, relativeTo: .headline))
                    .foregroundStyle(Theme.foreground)
                Spacer()
                Text(verbatim: "\(count)")
                    .font(.axMono(13, relativeTo: .footnote))
                    .foregroundStyle(Theme.muted)
            }
            VStack(spacing: 0) {
                Group(subviews: rows()) { subviews in
                    ForEach(subviews.indices, id: \.self) { index in
                        if index > 0 {
                            Divider().overlay(Theme.line)
                        }
                        subviews[index]
                    }
                }
            }
        }
        .surface(padding: 16)
    }
}

private struct StartGameSheet: View {
    let game: GameKind
    @Bindable var lobby: LobbyViewModel
    @Environment(\.dismiss) private var dismiss
    @State private var joining = false
    @FocusState private var linkFocused: Bool

    private var emptyLink: Bool { lobby.joinID.trimmingCharacters(in: .whitespaces).isEmpty }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 10) {
                    StartOption(
                        title: "Quick match",
                        subtitle: "Rated · your rating \(lobby.rating(game))",
                        systemImage: "bolt"
                    ) {
                        dismiss()
                        Task { await lobby.quickMatch(game) }
                    }
                    .accessibilityIdentifier(AccessibilityID.Lobby.quickMatch(game.rawValue))
                    StartOption(
                        title: "Private game",
                        subtitle: "Unrated · share the link with a friend",
                        systemImage: "link"
                    ) {
                        dismiss()
                        Task { await lobby.create(game) }
                    }
                    .accessibilityIdentifier(AccessibilityID.Lobby.create(game.rawValue))
                    StartOption(
                        title: "Practice offline",
                        subtitle: game == .durak ? "Against the computer" : "Against the computer or on one device",
                        systemImage: "cpu"
                    ) {
                        dismiss()
                        lobby.openPractice(game)
                    }
                    .accessibilityIdentifier(AccessibilityID.Lobby.practice(game.rawValue))
                    StartOption(
                        title: "Join with a link",
                        subtitle: "Paste a game link or ID from a friend",
                        systemImage: "arrow.down.left"
                    ) {
                        withAnimation(.snappy) { joining = true }
                        linkFocused = true
                    }
                    if joining {
                        HStack(spacing: 10) {
                            TextField("Game link or ID", text: $lobby.joinID)
                                .font(.axBody)
                                .autocorrectionDisabled()
                                #if os(iOS)
                                .textInputAutocapitalization(.never)
                                #endif
                                .focused($linkFocused)
                                .submitLabel(.join)
                                .onSubmit(join)
                                .padding(.horizontal, 12)
                                .padding(.vertical, 10)
                                .background(Theme.card, in: .rect(cornerRadius: 10, style: .continuous))
                                .overlay {
                                    RoundedRectangle(cornerRadius: 10, style: .continuous)
                                        .strokeBorder(Theme.border, lineWidth: 1)
                                }
                                .accessibilityIdentifier(AccessibilityID.Lobby.joinID)
                            Button("Join", action: join)
                                .buttonStyle(.borderedProminent)
                                .buttonBorderShape(.roundedRectangle(radius: 10))
                                .controlSize(.large)
                                .disabled(emptyLink || lobby.busy != nil)
                                .accessibilityIdentifier(AccessibilityID.Lobby.join)
                        }
                        .transition(.opacity.combined(with: .move(edge: .top)))
                    }
                }
                .padding(16)
            }
            .background(Theme.popover)
            .navigationTitle("Start a game")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close", systemImage: "xmark") { dismiss() }
                }
            }
        }
        .presentationDetents(joining ? [.large] : [.medium, .large])
        .presentationBackground(Theme.popover)
    }

    private func join() {
        guard !emptyLink else { return }
        dismiss()
        Task { await lobby.join() }
    }
}

private struct StartOption: View {
    let title: String
    let subtitle: String
    let systemImage: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 14) {
                Image(systemName: systemImage)
                    .font(.system(size: 18))
                    .foregroundStyle(Theme.primary)
                    .frame(width: 44, height: 44)
                    .background(Theme.accent, in: .circle)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.geist(15, .semibold, relativeTo: .body))
                        .foregroundStyle(Theme.foreground)
                    Text(subtitle)
                        .font(.axFootnote)
                        .foregroundStyle(Theme.muted)
                        .multilineTextAlignment(.leading)
                }
                Spacer(minLength: 0)
            }
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.card, in: .rect(cornerRadius: 14, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .strokeBorder(Theme.border, lineWidth: 1)
            }
            .contentShape(.rect(cornerRadius: 14))
        }
        .buttonStyle(.plain)
    }
}

struct MatchingBanner: View {
    let game: GameKind
    let lobby: LobbyViewModel

    var body: some View {
        HStack(spacing: 14) {
            ProgressView()
            VStack(alignment: .leading, spacing: 2) {
                Text("Finding an opponent")
                    .font(.axHeadline)
                Text(game.title)
                    .font(.axSubheadline)
                    .foregroundStyle(Theme.muted)
            }
            Spacer(minLength: 0)
            Button("Cancel") { Task { await lobby.cancelMatch() } }
                .buttonStyle(.glass)
                .accessibilityIdentifier(AccessibilityID.Lobby.cancelMatch)
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 14)
        .glassEffect(.regular, in: .rect(cornerRadius: 28, style: .continuous))
        .padding(.horizontal, 16)
        .padding(.bottom, 8)
    }
}
