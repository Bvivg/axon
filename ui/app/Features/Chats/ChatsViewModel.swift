import Foundation
import Observation

@MainActor
@Observable
final class ChatsViewModel {
    private(set) var rooms: [Room] = []
    private(set) var people: [String: PublicProfile] = [:]
    private(set) var isLoading = false
    private(set) var loaded = false
    private(set) var error: String?
    var query = ""
    let me: String
    @ObservationIgnored private let services: Services

    init(services: Services, me: String) {
        self.services = services
        self.me = me
    }

    var visible: [Room] {
        let needle = query.trimmingCharacters(in: .whitespaces)
        guard !needle.isEmpty else { return rooms }
        return rooms.filter { name(of: $0).localizedStandardContains(needle) }
    }

    func name(of room: Room) -> String {
        guard room.isDirect else { return room.name.isEmpty ? "Group" : room.name }
        return people[room.peerUserID]?.shownName ?? "Direct chat"
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

    func refresh() async {
        isLoading = true
        defer {
            isLoading = false
            loaded = true
        }
        do {
            let fresh = try await services.chat.rooms()
            rooms = fresh.sorted(by: Self.latestFirst)
            error = nil
        } catch {
            self.error = error.code == .unavailable ? "The server is unreachable." : "Could not load chats."
            return
        }
        let peers = rooms.filter(\.isDirect).map(\.peerUserID).filter { !$0.isEmpty }
        if let profiles = try? await services.auth.profiles(ids: peers) {
            people.merge(profiles) { _, new in new }
        }
    }

    private static func latestFirst(_ lhs: Room, _ rhs: Room) -> Bool {
        let left = lhs.hasLastMessage ? lhs.lastMessage.sentAt.seconds : lhs.createdAt.seconds
        let right = rhs.hasLastMessage ? rhs.lastMessage.sentAt.seconds : rhs.createdAt.seconds
        if left != right { return left > right }
        return lhs.id > rhs.id
    }
}
