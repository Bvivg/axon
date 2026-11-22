import SwiftUI

struct PaletteItem: Identifiable, Hashable {
    let id: String
    let title: String
    let subtitle: String
    let symbol: String
    let link: AppLink
}

extension ChatsViewModel {
    func title(of room: Room) -> String {
        if room.isDirect { return people[room.peerUserID]?.shownName ?? "Direct chat" }
        return room.name.isEmpty ? "Group" : room.name
    }
}

enum PaletteCatalog {
    @MainActor
    static func items(chats: ChatsViewModel) -> [PaletteItem] {
        let sections = AppTab.allCases.map { tab in
            item("tab:\(tab.rawValue)", tab.rawValue.capitalized, "Section", tab.symbol, .open(tab, []))
        }
        let actions = [
            item("new-chat", "New chat", "Chats", "square.and.pencil", .newChat),
            item("tournaments", "Tournaments", "Brackets and Swiss", "trophy", .open(.games, [.tournaments])),
        ]
        let games = GameKind.allCases.flatMap { game in
            [
                item("lobby:\(game.rawValue)", game.title, "Game", game.symbol, .open(.games, [.lobby(game)])),
                item("quick:\(game.rawValue)", "Quick match · \(game.title)", "Rated", "bolt", .quickMatch(game)),
                item(
                    "practice:\(game.rawValue)", "Practice · \(game.title)", "Offline", "cpu",
                    .open(.games, [.lobby(game), .practice(game)])
                ),
            ]
        }
        let rooms = chats.rooms.map { room in
            item(
                "room:\(room.id)", chats.title(of: room), room.isDirect ? "Chat" : "Group",
                room.isDirect ? "person" : "person.3", .open(.chats, [.room(room.id)])
            )
        }
        return sections + actions + games + rooms
    }

    private static func item(_ id: String, _ title: String, _ subtitle: String, _ symbol: String, _ link: AppLink) -> PaletteItem {
        PaletteItem(id: id, title: title, subtitle: subtitle, symbol: symbol, link: link)
    }
}

extension AppTab {
    var symbol: String {
        switch self {
        case .games: "gamecontroller"
        case .calls: "phone"
        case .chats: "message"
        case .profile: "person"
        }
    }
}

struct CommandPalette: View {
    let items: [PaletteItem]
    let choose: (AppLink) -> Void
    @State private var query = ""
    @FocusState private var focused: Bool
    @Environment(\.dismiss) private var dismiss

    private var matches: [PaletteItem] {
        let needle = query.trimmingCharacters(in: .whitespaces)
        guard !needle.isEmpty else { return Array(items.prefix(12)) }
        return items.filter { $0.title.localizedStandardContains(needle) || $0.subtitle.localizedStandardContains(needle) }
    }

    var body: some View {
        NavigationStack {
            List(matches) { item in
                Button {
                    dismiss()
                    choose(item.link)
                } label: {
                    CatalogRow(title: item.title, subtitle: item.subtitle) {
                        IconTile(size: 32) { Image(systemName: item.symbol).font(.system(size: 14)) }
                    }
                }
                .buttonStyle(.plain)
                .plainRow()
            }
            .listStyle(.plain)
            .themedList()
            .overlay {
                if matches.isEmpty {
                    EmptyState(title: "Nothing matches “\(query)”", systemImage: "magnifyingglass")
                }
            }
            .safeAreaInset(edge: .top) {
                TextField("Search games, chats and actions", text: $query)
                    .textFieldStyle(.plain)
                    .font(.axBody)
                    .focused($focused)
                    .onSubmit {
                        guard let first = matches.first else { return }
                        dismiss()
                        choose(first.link)
                    }
                    .padding(12)
                    .background(Theme.card, in: .rect(cornerRadius: 12, style: .continuous))
                    .overlay { RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Theme.border, lineWidth: 1) }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)
                    .accessibilityIdentifier(AccessibilityID.Home.palette)
            }
            .navigationTitle("Search")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close") { dismiss() }
                        .keyboardShortcut(.cancelAction)
                }
            }
        }
        .onAppear { focused = true }
        #if os(macOS)
        .frame(minWidth: 520, minHeight: 440)
        #endif
    }
}
