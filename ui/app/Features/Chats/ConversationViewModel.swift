import Foundation
import Observation

@MainActor
@Observable
final class ConversationViewModel: ChatEventListener {
    static let pageSize: Int32 = 50
    static let maxLength = 65_536
    static let typingInterval: TimeInterval = 3
    static let jumpPages = 20
    static let notAMember = "not_a_member"

    private(set) var roomID: String?
    let peer: String?
    private(set) var details: RoomDetails?
    private(set) var timeline = ChatTimeline()
    private(set) var rows: [TimelineRow] = []
    private(set) var hasOlder = false
    private(set) var loadingOlder = false
    private(set) var loaded = false
    private(set) var error: String?
    var notice: String?
    private(set) var removed = false
    private(set) var readSeqs: [String: Int64] = [:]
    var highlighted: String?
    private(set) var firstUnread: String?
    var editing: ChatMessage?
    var searchResults: [ChatMessage] = []
    var searching = false
    var sendingVoice = 0
    var scrollTarget: String?
    var draft = "" {
        didSet { if draft != oldValue, !draft.isEmpty { typed() } }
    }
    var replyTo: ChatMessage?
    var selection: Set<String>?
    let me: String
    let outbox: Outbox
    let tray: AttachmentTray
    @ObservationIgnored let hub: ChatsViewModel
    @ObservationIgnored private var lastTyping = Date.distantPast
    @ObservationIgnored private var markedSeq: Int64 = 0
    @ObservationIgnored var pendingEdits: [String: String] = [:]
    @ObservationIgnored private var visible = false
    @ObservationIgnored private var reloading = false

    init(target: ChatTarget, hub: ChatsViewModel, sendTimeout: Duration = .seconds(30)) {
        self.hub = hub
        me = hub.me
        outbox = Outbox(timeout: sendTimeout)
        tray = AttachmentTray(media: hub.services.media)
        switch target {
        case .room(let id):
            roomID = id
            let room = hub.room(id)
            peer = room?.isDirect == true ? room?.peerUserID : nil
        case .person(let user):
            roomID = nil
            peer = user
        }
    }

    var services: Services { hub.services }

    var target: ChatTarget? { roomID.map(ChatTarget.room) ?? peer.map(ChatTarget.person) }

    func start() async {
        hub.listen(self)
        if loaded, roomID != nil {
            await loadNewest()
            return
        }
        await hub.ensureProfiles(peer.map { [$0] } ?? [])
        if roomID == nil, let peer {
            do throws(APIError) {
                if let room = try await services.chat.directRoom(with: peer) {
                    hub.adopt(room)
                    roomID = room.id
                }
            } catch {
                self.error = Self.message(error)
            }
        }
        guard roomID != nil else {
            loaded = true
            return
        }
        if visible { hub.activeRoom = roomID }
        async let info: Void = loadDetails()
        await loadNewest(initial: true)
        await info
    }

    func stop() {
        hub.stopListening(self)
        disappear()
        outbox.cancelAll()
    }

    func appear() {
        visible = true
        if let roomID { hub.activeRoom = roomID }
        Task { await markRead() }
    }

    func disappear() {
        visible = false
        if hub.activeRoom == roomID { hub.activeRoom = nil }
    }

    func loadDetails() async {
        guard let roomID else { return }
        do throws(APIError) {
            let details = try await services.chat.room(id: roomID)
            self.details = details
            await hub.ensureProfiles(details.members.map(\.userID))
            for member in details.members where member.userID != me {
                readSeqs[member.userID] = max(readSeqs[member.userID] ?? 0, member.lastReadSeq)
            }
        } catch {
            if error.code == .notFound || error.code == .permissionDenied { removed = true }
        }
    }

    func loadNewest(initial: Bool = false) async {
        guard let roomID, !reloading else { return }
        reloading = true
        defer { reloading = false }
        do throws(APIError) {
            let page = try await services.chat.messages(in: roomID, HistoryQuery(limit: Self.pageSize))
            if initial || page.messages.first.map({ $0.seq > timeline.newestSeq + 1 }) ?? false {
                timeline.replace(with: page.messages)
                hasOlder = page.hasMore
            } else {
                timeline.refresh(with: page.messages)
            }
            if initial { anchorUnread() }
            error = nil
            rebuild()
            await hub.ensureProfiles(Array(Set(timeline.messages.map(\.authorID))))
        } catch {
            self.error = Self.message(error)
        }
        loaded = true
        await markRead()
    }

    func loadOlder() async {
        guard let roomID, hasOlder, !loadingOlder, let oldest = timeline.oldestSeq else { return }
        loadingOlder = true
        defer { loadingOlder = false }
        do throws(APIError) {
            let page = try await services.chat.messages(in: roomID, HistoryQuery(before: oldest, limit: Self.pageSize))
            timeline.merge(page.messages)
            hasOlder = page.hasMore
            rebuild()
            await hub.ensureProfiles(Array(Set(page.messages.map(\.authorID))))
        } catch {
            notice = "Could not load older messages."
        }
    }

    func markRead() async {
        guard visible, hub.sceneActive, let roomID else { return }
        let latest = timeline.newestSeq
        let previous = markedSeq
        guard latest > previous else { return }
        markedSeq = latest
        hub.markedRead(roomID, seq: latest)
        do throws(APIError) {
            try await services.chat.markRead(room: roomID, seq: latest)
        } catch {
            if markedSeq == latest { markedSeq = previous }
        }
    }

    func handle(_ event: ChatEvent) {
        switch event {
        case .message(let message):
            received(message)
        case .updated(let message):
            updated(message)
        case .ack(let room, let clientID, let seq):
            if outbox.acknowledge(clientID, seq: seq), roomID == nil { adopt(room) }
        case .failure(let code, let reason, let clientID):
            failed(code: code, reason: reason, clientID: clientID)
        case .state(.connecting):
            dropped()
        default:
            signal(event)
        }
    }

    private func signal(_ event: ChatEvent) {
        switch event {
        case .read(let room, let user, let seq):
            guard room == roomID, user != me else { return }
            readSeqs[user] = max(readSeqs[user] ?? 0, seq)
        case .subscribed(let room, let seq, let behind):
            guard room == roomID, loaded, behind || seq > timeline.newestSeq else { return }
            Task { await loadNewest() }
        case .roomRemoved(let room):
            if room == roomID { removed = true }
        default:
            break
        }
    }

    private func received(_ message: ChatMessage) {
        if message.hasClientID, outbox.confirm(message.clientID), roomID == nil {
            adopt(message.roomID)
        }
        guard message.roomID == roomID, !timeline.merge([message]).isEmpty else { return }
        rebuild()
        Task {
            await hub.ensureProfiles([message.authorID])
            await markRead()
        }
    }

    private func updated(_ message: ChatMessage) {
        guard message.roomID == roomID else { return }
        for (client, id) in pendingEdits where id == message.id { pendingEdits[client] = nil }
        if timeline.apply(message) { rebuild() }
    }

    private func adopt(_ room: String) {
        guard roomID == nil, !room.isEmpty else { return }
        roomID = room
        if visible { hub.activeRoom = room }
        Task {
            await hub.refresh()
            await loadDetails()
            await loadNewest()
        }
    }

    private func failed(code: String, reason: String, clientID: String?) {
        if let clientID, outbox.fail(clientID, reason: reason) { return }
        if let clientID, pendingEdits.removeValue(forKey: clientID) != nil {
            notice = reason.isEmpty ? "Your change was not saved." : reason
        } else if (clientID ?? "").isEmpty, code == Self.notAMember {
            return
        } else if !reason.isEmpty {
            notice = reason
        }
    }

    private func dropped() {
        outbox.dropped()
        guard !pendingEdits.isEmpty else { return }
        pendingEdits = [:]
        notice = "The connection dropped before your change was confirmed. Check the chat before trying again."
    }

    private func typed() {
        guard editing == nil, let roomID, hub.isSubscribed(roomID), Date.now.timeIntervalSince(lastTyping) >= Self.typingInterval
        else { return }
        lastTyping = .now
        Task { _ = await hub.send(.typing(room: roomID)) }
    }

    private func rebuild() {
        rows = TimelineRow.rows(for: timeline.messages)
        for item in outbox.items where timeline.contains(clientID: item.id) {
            outbox.remove(item.id)
        }
    }
}

extension ConversationViewModel {
    fileprivate func anchorUnread() {
        guard let room, room.unreadCount > 0 else { return }
        let first = timeline.newestSeq - room.unreadCount + 1
        firstUnread = timeline.messages.first { $0.seq >= first && $0.authorID != me }?.id
    }

    nonisolated static func message(_ error: APIError) -> String {
        switch error.code {
        case .unavailable: "The server is unreachable."
        case .notFound: "This chat no longer exists."
        case .permissionDenied: "You are not a member of this chat."
        case .resourceExhausted: "Slow down and try again in a moment."
        default: error.message.isEmpty ? "Something went wrong." : error.message
        }
    }
}
