import SwiftUI

struct GamesCatalogView: View {
    @Bindable var lobby: LobbyViewModel
    @State private var query = ""

    private var games: [GameKind] {
        let needle = query.trimmingCharacters(in: .whitespaces)
        guard !needle.isEmpty else { return GameKind.allCases }
        return GameKind.allCases.filter { $0.title.localizedStandardContains(needle) }
    }

    private var continuing: [Session] {
        query.isEmpty ? Array(lobby.active.prefix(4)) : []
    }

    var body: some View {
        List {
            if let error = lobby.error {
                ErrorBanner(text: error)
                    .accessibilityIdentifier(AccessibilityID.Lobby.error)
                    .plainRow()
            }
            if !continuing.isEmpty {
                Section {
                    ForEach(continuing, id: \.id) { session in
                        SessionRow(session: session, title: lobby.opponentName(in: session), me: lobby.me) {
                            lobby.openGame(session.id)
                        }
                        .plainRow()
                    }
                } header: {
                    Eyebrow("In progress")
                }
            }
            Section {
                ForEach(games) { game in
                    NavigationLink(value: Route.lobby(game)) {
                        CatalogRow(
                            title: game.title,
                            subtitle: "\(game.players) · Rating \(lobby.rating(game))"
                        ) {
                            GameBadge(kind: game)
                        }
                    }
                    .accessibilityIdentifier(AccessibilityID.Lobby.game(game.rawValue))
                    .plainRow()
                }
                if games.isEmpty {
                    Text("No games match “\(query)”.")
                        .font(.axFootnote)
                        .foregroundStyle(Theme.muted)
                        .plainRow()
                }
            } header: {
                Eyebrow("Catalogue")
            }
            if query.isEmpty || "tournaments".localizedStandardContains(query.trimmingCharacters(in: .whitespaces)) {
                Section {
                    NavigationLink(value: Route.tournaments) {
                        CatalogRow(title: "Tournaments", subtitle: "Brackets and Swiss") {
                            IconTile { Image(systemName: "trophy").font(.system(size: 16)) }
                        }
                    }
                    .accessibilityIdentifier(AccessibilityID.Tournaments.entry)
                    .plainRow()
                } header: {
                    Eyebrow("Compete")
                }
            }
        }
        .listStyle(.plain)
        .themedList(Theme.sidebar)
        .rootTitle("Games")
        .searchField($query, prompt: "Find a game")
        .refreshable { await lobby.refresh() }
        .accessibilityIdentifier(AccessibilityID.Lobby.root)
    }
}

struct CatalogRow<Leading: View>: View {
    let title: String
    var subtitle: String?
    @ViewBuilder let leading: () -> Leading

    var body: some View {
        HStack(spacing: 12) {
            leading()
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.geist(15, .semibold, relativeTo: .body))
                    .foregroundStyle(Theme.foreground)
                if let subtitle {
                    Text(subtitle)
                        .font(.axFootnote)
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }
}

struct ErrorBanner: View {
    let text: String

    var body: some View {
        Label(text, systemImage: "exclamationmark.triangle")
            .font(.axFootnote)
            .foregroundStyle(Theme.destructive)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(12)
            .background(Theme.destructive.opacity(0.1), in: .rect(cornerRadius: 12, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(Theme.destructive.opacity(0.35), lineWidth: 1)
            }
    }
}

extension View {
    func plainRow() -> some View {
        self
            .listRowBackground(Color.clear)
            .listRowSeparator(.hidden)
            .listRowInsets(EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16))
    }
}

extension GameKind {
    var players: String { "2 players" }
}
