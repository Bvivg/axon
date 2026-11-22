import SwiftUI

struct ChatsView: View {
    @Bindable var model: ChatsViewModel
    @State private var composing: NewChatSheet.Mode?
    @State private var hiding: Room?

    var body: some View {
        List {
            if let error = model.error {
                ErrorBanner(text: error).plainRow()
            }
            ForEach(model.visible, id: \.id) { room in
                NavigationLink(value: Route.room(room.id)) {
                    RoomRow(room: room, model: model)
                }
                .accessibilityIdentifier(AccessibilityID.Chats.room(room.id))
                .plainRow()
                .swipeActions(edge: .trailing) {
                    Button(room.isDirect ? "Delete" : "Leave", systemImage: "trash", role: .destructive) { hiding = room }
                }
            }
            if !model.query.trimmingCharacters(in: .whitespaces).isEmpty {
                Section {
                    ForEach(model.found.filter { !model.directPeers.contains($0.id) }, id: \.id) { person in
                        Button {
                            model.openPerson(person.id)
                        } label: {
                            CatalogRow(title: person.shownName, subtitle: person.hasUsername ? "@\(person.username)" : nil) {
                                Avatar(name: person.shownName, url: person.avatarLink, size: 44, cornerRadius: 22)
                            }
                        }
                        .buttonStyle(.plain)
                        .accessibilityIdentifier(AccessibilityID.Chats.person(person.id))
                        .plainRow()
                    }
                } header: {
                    if model.searchingPeople || !model.found.isEmpty {
                        Eyebrow("People")
                    }
                }
            }
        }
        .listStyle(.plain)
        .themedList(Theme.sidebar)
        .overlay {
            if model.visible.isEmpty, model.found.isEmpty, model.error == nil {
                if !model.loaded {
                    ProgressView()
                } else if model.query.isEmpty {
                    EmptyState(title: "No chats yet", systemImage: "message")
                } else if !model.searchingPeople {
                    EmptyState(title: "Nothing matches “\(model.query)”", systemImage: "magnifyingglass")
                }
            }
        }
        .rootTitle("Chats")
        .searchField($model.query, prompt: "Search chats and people")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button("New message", systemImage: "person") { composing = .direct }
                    Button("New group", systemImage: "person.3") { composing = .group }
                } label: {
                    Label("New chat", systemImage: "square.and.pencil")
                }
                .accessibilityIdentifier(AccessibilityID.Chats.compose)
            }
        }
        .sheet(item: $composing) { mode in
            NewChatSheet(hub: model, mode: mode)
        }
        .confirmationDialog(
            hiding?.isDirect == true ? "Delete this chat from your list?" : "Leave this group?",
            isPresented: Binding(get: { hiding != nil }, set: { if !$0 { hiding = nil } }),
            titleVisibility: .visible
        ) {
            Button(hiding?.isDirect == true ? "Delete chat" : "Leave group", role: .destructive) {
                guard let room = hiding else { return }
                hiding = nil
                Task {
                    if room.isDirect {
                        await model.hide(room)
                    } else {
                        try? await model.services.chat.leave(room: room.id)
                        model.forget(room.id)
                    }
                }
            }
        }
        .refreshable { await model.refresh() }
        .accessibilityIdentifier(AccessibilityID.Chats.root)
    }
}

extension NewChatSheet.Mode: Identifiable {
    var id: Self { self }
}

private struct RoomRow: View {
    let room: Room
    let model: ChatsViewModel

    var body: some View {
        let name = model.name(of: room)
        HStack(spacing: 12) {
            Avatar(name: name, url: model.avatar(of: room), size: 44)
                .overlay(alignment: .bottomTrailing) {
                    if model.isOnline(room) {
                        Circle()
                            .fill(Theme.success)
                            .frame(width: 12, height: 12)
                            .overlay { Circle().strokeBorder(Theme.sidebar, lineWidth: 2) }
                            .offset(x: 2, y: 2)
                    }
                }
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline) {
                    Text(name)
                        .font(.geist(15, .semibold, relativeTo: .body))
                        .foregroundStyle(Theme.foreground)
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    Text(Clock.short(room.lastActivity()))
                        .font(.axMono(11, relativeTo: .caption2))
                        .foregroundStyle(Theme.muted)
                }
                HStack(spacing: 4) {
                    if let typing = model.typingText(in: room) {
                        Text(typing)
                            .font(.axFootnote)
                            .foregroundStyle(Theme.primary)
                            .lineLimit(1)
                    } else {
                        if room.hasLastMessage, model.isMine(room.lastMessage) {
                            Image(systemName: model.isRead(room) ? "checkmark.circle.fill" : "checkmark")
                                .font(.system(size: 11, weight: .semibold))
                                .foregroundStyle(model.isRead(room) ? Theme.primary : Theme.muted)
                                .accessibilityLabel(model.isRead(room) ? "Read" : "Sent")
                        }
                        Text(preview)
                            .font(.axFootnote)
                            .foregroundStyle(Theme.muted)
                            .lineLimit(1)
                    }
                    Spacer(minLength: 8)
                    if room.unreadCount > 0 {
                        Text(room.unreadCount > 99 ? "99+" : "\(room.unreadCount)")
                            .font(.geist(11, .semibold, relativeTo: .caption2))
                            .foregroundStyle(Theme.onPrimary)
                            .padding(.horizontal, 6)
                            .frame(minWidth: 20, minHeight: 20)
                            .background(Theme.primary, in: .capsule)
                            .accessibilityIdentifier(AccessibilityID.Chats.unread(room.id))
                    }
                }
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }

    private var preview: String {
        guard room.hasLastMessage else { return "No messages yet" }
        let message = room.lastMessage
        if message.isSystem { return message.systemText(me: model.me, name: model.name(ofUser:)) }
        guard !room.isDirect, !message.isDeleted else { return message.preview }
        return "\(model.name(ofUser: message.authorID)): \(message.preview)"
    }
}
