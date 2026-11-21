import SwiftUI

struct ChatsView: View {
    @Bindable var model: ChatsViewModel

    var body: some View {
        List {
            if let error = model.error {
                ErrorBanner(text: error).plainRow()
            }
            ForEach(model.visible, id: \.id) { room in
                RoomRow(room: room, model: model)
                    .accessibilityIdentifier(AccessibilityID.Chats.room(room.id))
                    .plainRow()
            }
        }
        .listStyle(.plain)
        .themedList(Theme.sidebar)
        .overlay {
            if model.visible.isEmpty, model.error == nil {
                if !model.loaded {
                    ProgressView()
                } else if model.query.isEmpty {
                    EmptyState(title: "No chats yet", systemImage: "message")
                } else {
                    EmptyState(title: "Nothing matches “\(model.query)”", systemImage: "magnifyingglass")
                }
            }
        }
        .rootTitle("Chats")
        .searchField($model.query, prompt: "Search chats and people")
        .refreshable { await model.refresh() }
        .task { await model.refresh() }
        .accessibilityIdentifier(AccessibilityID.Chats.root)
    }
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
                    if room.hasLastMessage, model.isMine(room.lastMessage) {
                        Image(systemName: model.isRead(room) ? "checkmark.circle.fill" : "checkmark")
                            .font(.system(size: 11, weight: .semibold))
                            .foregroundStyle(model.isRead(room) ? Theme.primary : Theme.muted)
                            .accessibilityLabel(model.isRead(room) ? "Read" : "Sent")
                    }
                    Text(room.hasLastMessage ? room.lastMessage.preview : "No messages yet")
                        .font(.axFootnote)
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    if room.unreadCount > 0 {
                        Text(room.unreadCount > 99 ? "99+" : "\(room.unreadCount)")
                            .font(.geist(11, .semibold, relativeTo: .caption2))
                            .foregroundStyle(Theme.onPrimary)
                            .padding(.horizontal, 6)
                            .frame(minWidth: 20, minHeight: 20)
                            .background(Theme.primary, in: .capsule)
                    }
                }
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }
}
