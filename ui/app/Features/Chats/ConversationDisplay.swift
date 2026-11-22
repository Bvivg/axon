import Foundation

extension ConversationViewModel {
    var room: Room? { roomID.flatMap(hub.room) ?? details?.room }

    var isDirect: Bool { peer != nil || room?.isDirect == true }

    var peerID: String? {
        if let peer { return peer }
        guard let room, room.isDirect else { return nil }
        return room.peerUserID
    }

    var title: String {
        if let peerID { return hub.people[peerID]?.shownName ?? "Direct chat" }
        guard let room else { return "Chat" }
        return room.name.isEmpty ? "Group" : room.name
    }

    var avatar: URL? { peerID.flatMap { hub.people[$0]?.avatarLink } }

    var subtitle: String? {
        if let roomID, let typing = typingText(in: roomID) { return typing }
        if let peerID { return ChatFormat.lastSeen(hub.people[peerID]) }
        let count = details?.members.count ?? Int(room?.memberCount ?? 0)
        return count > 0 ? "\(count) members" : nil
    }

    var members: [RoomMember] { details?.members ?? [] }

    var canSend: Bool {
        guard !tray.busy, !removed else { return false }
        let body = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        if let editing {
            return body != editing.body.trimmingCharacters(in: .whitespacesAndNewlines) && (!body.isEmpty || editing.content != .text)
        }
        return !body.isEmpty || !tray.isEmpty
    }

    var pendingOutgoing: [OutgoingMessage] {
        outbox.items.filter { !timeline.contains(clientID: $0.id) }
    }

    func name(of user: String) -> String {
        if user == me { return "You" }
        if let profile = hub.people[user] { return profile.shownName }
        let member = members.first { $0.userID == user && $0.hasDisplayName && !$0.displayName.isEmpty }
        return member?.displayName ?? "Member"
    }

    func isMine(_ message: ChatMessage) -> Bool {
        message.authorID == me && !message.isSystem
    }
}
