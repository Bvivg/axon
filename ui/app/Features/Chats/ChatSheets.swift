import SwiftUI

struct ForwardSheet: View {
    let hub: ChatsViewModel
    let count: Int
    let send: ([String], String) async -> Bool
    @Environment(\.dismiss) private var dismiss
    @State private var picked: Set<String> = []
    @State private var comment = ""
    @State private var query = ""
    @State private var sending = false

    private var rooms: [Room] {
        let needle = query.trimmingCharacters(in: .whitespaces)
        return needle.isEmpty ? hub.rooms : hub.rooms.filter { hub.name(of: $0).localizedStandardContains(needle) }
    }

    var body: some View {
        NavigationStack {
            List {
                ForEach(rooms, id: \.id) { room in
                    Button {
                        if picked.contains(room.id) { picked.remove(room.id) } else { picked.insert(room.id) }
                    } label: {
                        HStack(spacing: 12) {
                            Avatar(name: hub.name(of: room), url: hub.avatar(of: room), size: 36)
                            Text(hub.name(of: room))
                                .font(.geist(15, .medium, relativeTo: .body))
                                .foregroundStyle(Theme.foreground)
                            Spacer()
                            Image(systemName: picked.contains(room.id) ? "checkmark.circle.fill" : "circle")
                                .foregroundStyle(picked.contains(room.id) ? Theme.primary : Theme.muted)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier(AccessibilityID.Chats.forwardTarget(room.id))
                    .plainRow()
                }
            }
            .listStyle(.plain)
            .themedList(Theme.popover)
            .searchable(text: $query, prompt: "Search chats")
            .searchPresentationToolbarBehavior(.avoidHidingContent)
            .safeAreaInset(edge: .bottom) {
                HStack(spacing: 8) {
                    TextField("Add a comment", text: $comment, axis: .vertical)
                        .font(.axBody)
                        .lineLimit(1...4)
                    Button {
                        sending = true
                        Task {
                            if await send(Array(picked), comment.trimmingCharacters(in: .whitespacesAndNewlines)) { dismiss() }
                            sending = false
                        }
                    } label: {
                        Image(systemName: "arrow.up")
                            .font(.system(size: 16, weight: .bold))
                            .foregroundStyle(Theme.onPrimary)
                            .frame(width: 36, height: 36)
                            .background(picked.isEmpty ? Theme.muted.opacity(0.5) : Theme.primary, in: .circle)
                    }
                    .buttonStyle(.plain)
                    .disabled(picked.isEmpty || sending)
                    .accessibilityLabel("Forward")
                    .accessibilityIdentifier(AccessibilityID.Chats.forwardSend)
                }
                .padding(12)
                .glassEffect(.regular, in: .rect(cornerRadius: 24, style: .continuous))
                .padding(10)
            }
            .navigationTitle(count == 1 ? "Forward message" : "Forward \(count) messages")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close", systemImage: "xmark") { dismiss() }
                }
            }
        }
        .presentationBackground(Theme.popover)
    }
}

struct ReadersSheet: View {
    let model: ConversationViewModel
    let message: ChatMessage
    @Environment(\.dismiss) private var dismiss
    @State private var readers: [MessageReader]?

    var body: some View {
        NavigationStack {
            List {
                if let readers {
                    ForEach(readers, id: \.userID) { reader in
                        HStack(spacing: 12) {
                            Avatar(
                                name: model.name(of: reader.userID),
                                url: model.hub.people[reader.userID]?.avatarLink,
                                size: 36,
                                cornerRadius: 18
                            )
                            Text(model.name(of: reader.userID))
                                .font(.geist(15, .medium, relativeTo: .body))
                                .foregroundStyle(Theme.foreground)
                            Spacer()
                            if reader.hasReadAt {
                                Text(Clock.short(reader.readAt.date))
                                    .font(.axMono(11, relativeTo: .caption2))
                                    .foregroundStyle(Theme.muted)
                            }
                        }
                        .plainRow()
                    }
                }
            }
            .listStyle(.plain)
            .themedList(Theme.popover)
            .overlay {
                if readers == nil {
                    ProgressView()
                } else if readers?.isEmpty == true {
                    EmptyState(title: "Nobody has read it yet", systemImage: "eye.slash")
                }
            }
            .navigationTitle("Seen by")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close", systemImage: "xmark") { dismiss() }
                }
            }
            .task { readers = await model.readers(of: message) }
        }
        .presentationDetents([.medium, .large])
        .presentationBackground(Theme.popover)
    }
}

struct MessageSearchSheet: View {
    let model: ConversationViewModel
    let pick: (ChatMessage) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""
    @FocusState private var typing: Bool

    var body: some View {
        NavigationStack {
            List {
                ForEach(model.searchResults, id: \.id) { message in
                    Button {
                        pick(message)
                    } label: {
                        VStack(alignment: .leading, spacing: 3) {
                            HStack {
                                Text(model.name(of: message.authorID))
                                    .font(.geist(13, .semibold, relativeTo: .footnote))
                                    .foregroundStyle(Theme.foreground)
                                Spacer()
                                Text(Clock.short(message.sentDate))
                                    .font(.axMono(11, relativeTo: .caption2))
                                    .foregroundStyle(Theme.muted)
                            }
                            Text(message.preview)
                                .font(.axSubheadline)
                                .foregroundStyle(Theme.muted)
                                .lineLimit(2)
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityIdentifier(AccessibilityID.Chats.searchResult(message.id))
                    .plainRow()
                }
            }
            .listStyle(.plain)
            .themedList(Theme.popover)
            .overlay {
                if model.searching {
                    ProgressView()
                } else if query.count < 2 {
                    EmptyState(title: "Search this chat", systemImage: "magnifyingglass")
                } else if model.searchResults.isEmpty {
                    EmptyState(title: "Nothing matches “\(query)”", systemImage: "magnifyingglass")
                }
            }
            .searchable(text: $query, prompt: "Search messages")
            .searchFocused($typing)
            .searchPresentationToolbarBehavior(.avoidHidingContent)
            .onAppear { typing = true }
            .task(id: query) {
                try? await Task.sleep(for: .milliseconds(300))
                guard !Task.isCancelled else { return }
                await model.search(query)
            }
            .navigationTitle("Search")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close", systemImage: "xmark") { dismiss() }
                }
            }
        }
        .presentationBackground(Theme.popover)
    }
}

struct PeoplePicker: View {
    let hub: ChatsViewModel
    let multiple: Bool
    let excluded: Set<String>
    @Binding var picked: [PublicProfile]
    let onPick: (PublicProfile) -> Void
    @State private var query = ""
    @State private var search: PeopleSearch

    init(
        hub: ChatsViewModel,
        multiple: Bool = false,
        excluded: Set<String> = [],
        picked: Binding<[PublicProfile]>,
        onPick: @escaping (PublicProfile) -> Void = { _ in }
    ) {
        self.hub = hub
        self.multiple = multiple
        self.excluded = excluded
        _picked = picked
        self.onPick = onPick
        _search = State(initialValue: PeopleSearch(auth: hub.services.auth, me: hub.me) { [weak hub] found in
            hub?.remember(found)
        })
    }

    private var results: [PublicProfile] { search.results }

    var body: some View {
        List {
            if multiple, !picked.isEmpty {
                ScrollView(.horizontal) {
                    HStack(spacing: 6) {
                        ForEach(picked, id: \.id) { person in
                            Button {
                                picked.removeAll { $0.id == person.id }
                            } label: {
                                Label(person.shownName, systemImage: "xmark")
                                    .font(.geist(13, .medium, relativeTo: .footnote))
                                    .labelStyle(TrailingIconLabel())
                                    .padding(.horizontal, 10)
                                    .padding(.vertical, 6)
                                    .background(Theme.accent, in: .capsule)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                }
                .scrollIndicators(.hidden)
                .plainRow()
            }
            ForEach(results.filter { !excluded.contains($0.id) }, id: \.id) { person in
                Button {
                    if multiple {
                        if picked.contains(where: { $0.id == person.id }) {
                            picked.removeAll { $0.id == person.id }
                        } else {
                            picked.append(person)
                        }
                    } else {
                        onPick(person)
                    }
                } label: {
                    HStack(spacing: 12) {
                        Avatar(name: person.shownName, url: person.avatarLink, size: 40, cornerRadius: 20)
                            .overlay(alignment: .bottomTrailing) {
                                if person.online {
                                    Circle().fill(Theme.success).frame(width: 10, height: 10)
                                }
                            }
                        VStack(alignment: .leading, spacing: 2) {
                            Text(person.shownName)
                                .font(.geist(15, .medium, relativeTo: .body))
                                .foregroundStyle(Theme.foreground)
                            if person.hasUsername, !person.username.isEmpty {
                                Text("@\(person.username)")
                                    .font(.axFootnote)
                                    .foregroundStyle(Theme.muted)
                            }
                        }
                        Spacer()
                        if multiple {
                            Image(systemName: picked.contains { $0.id == person.id } ? "checkmark.circle.fill" : "circle")
                                .foregroundStyle(picked.contains { $0.id == person.id } ? Theme.primary : Theme.muted)
                        }
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier(AccessibilityID.Chats.person(person.id))
                .plainRow()
            }
        }
        .listStyle(.plain)
        .themedList(Theme.popover)
        .overlay {
            if search.loading, results.isEmpty {
                ProgressView()
            } else if !search.loading, results.isEmpty {
                EmptyState(title: query.count < 2 ? "Search by name or username" : "Nobody found", systemImage: "person.2")
            }
        }
        .searchable(text: $query, prompt: "Name or username")
        .searchPresentationToolbarBehavior(.avoidHidingContent)
        .onChange(of: query) { _, text in search.update(text) }
    }
}

private struct TrailingIconLabel: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 4) {
            configuration.title
            configuration.icon.font(.system(size: 10, weight: .bold))
        }
    }
}

struct NewChatSheet: View {
    enum Mode {
        case direct
        case group
    }

    let hub: ChatsViewModel
    @State var mode: Mode
    @Environment(\.dismiss) private var dismiss
    @State private var picked: [PublicProfile] = []
    @State private var name = ""
    @State private var creating = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                if mode == .group {
                    VStack(alignment: .leading, spacing: 8) {
                        TextField("Group name", text: $name)
                            .font(.axBody)
                            .padding(.horizontal, 12)
                            .frame(minHeight: 44)
                            .background(Theme.card, in: .rect(cornerRadius: 10, style: .continuous))
                            .overlay { RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.border, lineWidth: 1) }
                            .accessibilityIdentifier(AccessibilityID.Chats.groupName)
                        if let error {
                            Text(error).font(.axFootnote).foregroundStyle(Theme.destructive)
                        }
                    }
                    .padding(16)
                }
                PeoplePicker(hub: hub, multiple: mode == .group, picked: $picked) { person in
                    dismiss()
                    hub.openPerson(person.id)
                }
            }
            .background(Theme.popover)
            .navigationTitle(mode == .group ? "New group" : "New message")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close", systemImage: "xmark") { dismiss() }
                }
                if mode == .group {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Create") { Task { await create() } }
                            .disabled(creating || picked.isEmpty || name.trimmingCharacters(in: .whitespaces).isEmpty)
                            .accessibilityIdentifier(AccessibilityID.Chats.createGroup)
                    }
                }
            }
        }
        .presentationBackground(Theme.popover)
    }

    private func create() async {
        creating = true
        defer { creating = false }
        do {
            try await hub.createGroup(name: name.trimmingCharacters(in: .whitespaces), members: picked.map(\.id))
            dismiss()
        } catch {
            self.error = error.message.isEmpty ? "Could not create the group." : error.message
        }
    }
}
