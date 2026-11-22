import Foundation

extension ChatsViewModel {
    var connected: Bool { connection == .open }

    var unread: Int { rooms.reduce(0) { $0 + Int($1.unreadCount) } }

    var found: [PublicProfile] { search.results }

    var searchingPeople: Bool { search.loading }

    var visible: [Room] {
        let needle = query.trimmingCharacters(in: .whitespaces)
        guard !needle.isEmpty else { return rooms }
        return rooms.filter { name(of: $0).localizedStandardContains(needle) }
    }

    var directPeers: Set<String> {
        Set(rooms.filter(\.isDirect).map(\.peerUserID))
    }

    func room(_ id: String) -> Room? {
        rooms.first { $0.id == id }
    }

    func name(of room: Room) -> String {
        guard room.isDirect else { return room.name.isEmpty ? "Group" : room.name }
        return people[room.peerUserID]?.shownName ?? "Direct chat"
    }

    func name(ofUser id: String) -> String {
        id == me ? "You" : people[id]?.shownName ?? "Member"
    }

    func avatar(of room: Room) -> URL? {
        room.isDirect ? people[room.peerUserID]?.avatarLink : nil
    }

    func isOnline(_ room: Room) -> Bool {
        room.isDirect && people[room.peerUserID]?.online == true
    }

    func isMine(_ message: ChatMessage) -> Bool {
        message.authorID == me && message.kind != .system
    }

    func isRead(_ room: Room) -> Bool {
        room.hasLastMessage && room.othersReadSeq >= room.lastMessage.seq
    }

    func typingText(in room: Room) -> String? {
        let others = typing.typing(in: room.id).filter { $0 != me }
        guard !others.isEmpty else { return nil }
        return room.isDirect ? "typing…" : ChatFormat.typing(others.map(name(ofUser:)))
    }

    func openPerson(_ user: String) {
        if let room = rooms.first(where: { $0.isDirect && $0.peerUserID == user }) {
            open(.room(room.id))
        } else {
            open(.person(user))
        }
    }

    func createGroup(name: String, members: [String]) async throws(APIError) {
        let room = try await services.chat.createGroup(name: name, members: members)
        adopt(room)
        open(.room(room.id))
    }
}
