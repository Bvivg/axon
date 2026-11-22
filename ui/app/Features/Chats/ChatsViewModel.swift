import Foundation
import Observation

@MainActor
protocol ChatEventListener: AnyObject {
    func handle(_ event: ChatEvent)
}

@MainActor
@Observable
final class ChatsViewModel {
    private(set) var rooms: [Room] = []
    private(set) var people: [String: PublicProfile] = [:]
    private(set) var isLoading = false
    private(set) var loaded = false
    private(set) var error: String?
    private(set) var connection: ChannelState = .connecting(attempt: 0)
    private(set) var typing = TypingTracker()
    var query = "" {
        didSet { if query != oldValue { search.update(query) } }
    }
    var activeRoom: String?
    var composing: NewChatSheet.Mode?
    var sceneActive = true
    var sceneVisible = true
    let me: String
    let search: PeopleSearch
    @ObservationIgnored let services: Services
    @ObservationIgnored var onRevoked: (() -> Void)?
    @ObservationIgnored let open: (Route) -> Void
    @ObservationIgnored private let alerts: (any ChatAlerting)?
    @ObservationIgnored private var socket: (any ChatSocket)?
    @ObservationIgnored private var book = SubscriptionBook()
    @ObservationIgnored private var listeners: [ObjectIdentifier: WeakListener] = [:]
    @ObservationIgnored private var refreshTask: Task<Void, Never>?
    @ObservationIgnored private var typingTask: Task<Void, Never>?
    @ObservationIgnored private var presenceTasks: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var everOpened = false
    @ObservationIgnored private var announced: Set<String> = []

    private struct WeakListener {
        weak var listener: (any ChatEventListener)?
    }

    init(services: Services, me: String, alerts: (any ChatAlerting)? = nil, open: @escaping (Route) -> Void = { _ in }) {
        self.services = services
        self.me = me
        self.alerts = alerts
        self.open = open
        search = PeopleSearch(auth: services.auth, me: me)
    }

    func run() async {
        alerts?.activate()
        await refresh()
        let socket = services.chatSocket.open()
        self.socket = socket
        for await event in socket.events {
            handle(event)
        }
        if self.socket === socket { self.socket = nil }
        connection = .closed
        await socket.close()
    }

    func refresh() async {
        isLoading = true
        defer {
            isLoading = false
            loaded = true
        }
        do throws(APIError) {
            let fetched = try await services.chat.rooms()
            rooms = Self.merge(fetched, keeping: rooms).sorted(by: Self.latestFirst)
            error = nil
        } catch {
            self.error = error.code == .unavailable ? "The server is unreachable." : "Could not load chats."
            return
        }
        book.keep(Set(rooms.map(\.id)))
        await ensureProfiles(rooms.flatMap(\.involvedUsers))
        syncSubscriptions()
    }

    func ensureProfiles(_ ids: [String]) async {
        let missing = Array(Set(ids.filter { !$0.isEmpty && people[$0] == nil }))
        guard !missing.isEmpty, let profiles = try? await services.auth.profiles(ids: missing) else { return }
        people.merge(profiles) { _, new in new }
    }

    func remember(_ profiles: [PublicProfile]) {
        for profile in profiles { people[profile.id] = profile }
    }

    func send(_ command: ChatCommand) async -> Bool {
        guard let socket else { return false }
        return await socket.send(command)
    }

    func listen(_ listener: any ChatEventListener) {
        listeners[ObjectIdentifier(listener)] = WeakListener(listener: listener)
    }

    func stopListening(_ listener: any ChatEventListener) {
        listeners[ObjectIdentifier(listener)] = nil
    }

    func adopt(_ room: Room) {
        if let index = rooms.firstIndex(where: { $0.id == room.id }) {
            rooms[index] = room
        } else {
            rooms.append(room)
        }
        rooms.sort(by: Self.latestFirst)
        syncSubscriptions()
    }

    func markedRead(_ room: String, seq: Int64) {
        guard let index = rooms.firstIndex(where: { $0.id == room }) else { return }
        if !rooms[index].hasLastMessage || seq >= rooms[index].lastMessage.seq {
            rooms[index].unreadCount = 0
        }
    }

    func forget(_ room: String) {
        rooms.removeAll { $0.id == room }
        guard book.subscribed.contains(room) else { return }
        book.drop(room)
        guard connected, let socket else { return }
        Task { _ = await socket.send(.unsubscribe(room: room)) }
    }

    func hide(_ room: Room) async {
        do throws(APIError) {
            try await services.chat.hide(room: room.id)
            forget(room.id)
        } catch {
            self.error = "Could not delete the chat."
        }
    }

    private func handle(_ event: ChatEvent) {
        apply(event)
        for (key, entry) in listeners {
            guard let listener = entry.listener else {
                listeners[key] = nil
                continue
            }
            listener.handle(event)
        }
    }

    private func apply(_ event: ChatEvent) {
        switch event {
        case .state(let state):
            changed(state)
        case .message(let message):
            book.saw(message.roomID, seq: message.seq)
            typing.stop(room: message.roomID, user: message.authorID)
            absorb(message)
        case .updated(let message):
            updated(message)
        case .roomAdded(let room, let message):
            added(room, message)
        case .roomRemoved(let room):
            forget(room)
        default:
            signal(event)
        }
    }

    private func changed(_ state: ChannelState) {
        connection = state
        guard state == .open else { return }
        book.reset()
        syncSubscriptions()
        if everOpened { refreshSoon() }
        everOpened = true
    }

    private func updated(_ message: ChatMessage) {
        guard let index = rooms.firstIndex(where: { $0.id == message.roomID }) else { return }
        if rooms[index].lastMessage.id == message.id { rooms[index].lastMessage = message }
    }

    private func added(_ room: String, _ message: ChatMessage?) {
        if let message { absorb(message) }
        if self.room(room) == nil { refreshSoon() } else { syncSubscriptions() }
    }

    private func signal(_ event: ChatEvent) {
        switch event {
        case .subscribed(let room, let seq, let behind):
            book.saw(room, seq: seq)
            if behind { refreshSoon() }
        case .read(let room, let user, let seq):
            read(room: room, user: user, seq: seq)
        case .typing(let room, let user):
            guard user != me else { return }
            typing.start(room: room, user: user)
            pruneTyping()
        case .presence(let user):
            refreshPresence(of: user)
        case .revoked:
            onRevoked?()
        default:
            break
        }
    }

    private func absorb(_ message: ChatMessage) {
        guard let index = rooms.firstIndex(where: { $0.id == message.roomID }) else {
            refreshSoon()
            Task {
                await ensureProfiles(message.involvedUsers)
                alert(message, in: nil)
            }
            return
        }
        var room = rooms[index]
        guard !room.hasLastMessage || message.seq > room.lastMessage.seq else { return }
        room.lastMessage = message
        let foreign = message.authorID != me && !message.isSystem
        if foreign, message.roomID != foregroundRoom {
            room.unreadCount += 1
        }
        rooms[index] = room
        rooms.sort(by: Self.latestFirst)
        Task {
            await ensureProfiles(message.involvedUsers)
            if foreign { alert(message, in: room) }
        }
    }

    private func read(room: String, user: String, seq: Int64) {
        guard let index = rooms.firstIndex(where: { $0.id == room }) else { return }
        if user == me {
            markedRead(room, seq: seq)
        } else if seq > rooms[index].othersReadSeq {
            rooms[index].othersReadSeq = seq
        }
    }

    private func syncSubscriptions() {
        guard connected, let socket else { return }
        let commands = book.pending(rooms: rooms.map(\.id), peers: directPeers)
        guard !commands.isEmpty else { return }
        Task {
            for command in commands {
                _ = await socket.send(command)
            }
        }
    }

    private func refreshSoon() {
        guard refreshTask == nil else { return }
        refreshTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(400))
            await self?.refresh()
            self?.refreshTask = nil
        }
    }

    private static func latestFirst(_ lhs: Room, _ rhs: Room) -> Bool {
        let left = lhs.hasLastMessage ? lhs.lastMessage.sentAt.seconds : lhs.createdAt.seconds
        let right = rhs.hasLastMessage ? rhs.lastMessage.sentAt.seconds : rhs.createdAt.seconds
        if left != right { return left > right }
        return lhs.id > rhs.id
    }
}

extension ChatsViewModel {
    var foregroundRoom: String? { sceneVisible ? activeRoom : nil }

    func isSubscribed(_ room: String) -> Bool {
        book.subscribed.contains(room)
    }

    func resume() {
        guard connection != .open, let socket else { return }
        Task { await socket.reconnect() }
    }

    fileprivate static func merge(_ fetched: [Room], keeping local: [Room]) -> [Room] {
        let known = Dictionary(local.map { ($0.id, $0) }) { first, _ in first }
        return fetched.map { room in
            guard let mine = known[room.id], mine.hasLastMessage,
                  !room.hasLastMessage || mine.lastMessage.seq > room.lastMessage.seq
            else { return room }
            var merged = room
            merged.lastMessage = mine.lastMessage
            merged.unreadCount = max(room.unreadCount, mine.unreadCount)
            return merged
        }
    }

    fileprivate func alert(_ message: ChatMessage, in room: Room?) {
        guard let alerts, message.authorID != me, message.roomID != foregroundRoom else { return }
        if announced.count > 500 { announced.removeAll() }
        guard announced.insert(message.id).inserted else { return }
        let author = people[message.authorID]?.shownName ?? "New message"
        let title = room.map { $0.isDirect ? author : "\(author) · \(name(of: $0))" } ?? author
        alerts.post(title: title, body: message.preview, room: message.roomID)
    }

    fileprivate func refreshPresence(of user: String) {
        presenceTasks[user]?.cancel()
        presenceTasks[user] = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(300))
            guard !Task.isCancelled, let self else { return }
            if let profile = try? await services.auth.profiles(ids: [user])[user] {
                people[user] = profile
            }
            presenceTasks[user] = nil
        }
    }

    fileprivate func pruneTyping() {
        guard typingTask == nil else { return }
        typingTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                guard let self else { return }
                typing.prune()
                if typing.expiries.isEmpty {
                    typingTask = nil
                    return
                }
            }
        }
    }
}
