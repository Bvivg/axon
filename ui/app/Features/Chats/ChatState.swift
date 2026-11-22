import Foundation
import Observation

@MainActor
@Observable
final class Outbox {
    private(set) var items: [OutgoingMessage] = []
    @ObservationIgnored let timeout: Duration
    @ObservationIgnored private var timers: [String: Task<Void, Never>] = [:]

    init(timeout: Duration) {
        self.timeout = timeout
    }

    func add(_ draft: OutgoingDraft, quote: Axon_Chat_V1_ReplyPreview?) {
        items.append(OutgoingMessage(draft: draft, status: .sending, createdAt: .now, replyPreview: quote))
    }

    func remove(_ id: String) {
        stopTimer(id)
        items.removeAll { $0.id == id }
    }

    func arm(_ id: String) {
        stopTimer(id)
        let timeout = timeout
        timers[id] = Task { [weak self] in
            try? await Task.sleep(for: timeout)
            guard !Task.isCancelled else { return }
            self?.unconfirm(id)
        }
    }

    func unconfirm(_ id: String) {
        stopTimer(id)
        guard let index = items.firstIndex(where: { $0.id == id }), items[index].status == .sending else { return }
        items[index].status = .unconfirmed
    }

    func resend(_ id: String) -> OutgoingDraft? {
        guard let index = items.firstIndex(where: { $0.id == id }), items[index].status == .unconfirmed else { return nil }
        items[index].status = .sending
        return items[index].draft
    }

    @discardableResult
    func acknowledge(_ id: String, seq: Int64) -> Bool {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return false }
        stopTimer(id)
        items[index].status = .sent
        items[index].seq = seq
        return true
    }

    @discardableResult
    func confirm(_ id: String) -> Bool {
        guard items.contains(where: { $0.id == id }) else { return false }
        remove(id)
        return true
    }

    @discardableResult
    func fail(_ id: String, reason: String) -> Bool {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return false }
        stopTimer(id)
        items[index].status = .failed(reason.isEmpty ? "The server rejected this message." : reason)
        return true
    }

    func dropped() {
        for item in items where item.status == .sending {
            unconfirm(item.id)
        }
    }

    func cancelAll() {
        for task in timers.values { task.cancel() }
        timers = [:]
    }

    private func stopTimer(_ id: String) {
        timers[id]?.cancel()
        timers[id] = nil
    }
}

@MainActor
@Observable
final class AttachmentTray {
    static let limit = 10

    private(set) var items: [PendingAttachment] = []
    @ObservationIgnored private let media: any MediaAPI
    @ObservationIgnored private var tasks: [String: Task<Void, Never>] = [:]

    init(media: any MediaAPI) {
        self.media = media
    }

    var isEmpty: Bool { items.isEmpty }

    var busy: Bool { items.contains { $0.uploadedID == nil } }

    var uploadedIDs: [String] { items.compactMap(\.uploadedID) }

    @discardableResult
    func attach(_ files: [URL], as purpose: UploadPurpose) -> Bool {
        let room = Self.limit - items.count
        for file in files.prefix(max(0, room)) {
            let id = UUID().uuidString
            items.append(PendingAttachment(id: id, file: file, name: file.lastPathComponent, purpose: purpose, phase: .uploading(.hashing)))
            start(id)
        }
        return files.count <= room
    }

    func remove(_ id: String) {
        tasks[id]?.cancel()
        tasks[id] = nil
        items.removeAll { $0.id == id }
    }

    func retry(_ id: String) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        items[index].phase = .uploading(.hashing)
        start(id)
    }

    func take() -> [PendingAttachment] {
        let taken = items
        items = []
        tasks = [:]
        return taken
    }

    func restore(_ taken: [PendingAttachment]) {
        items = taken
    }

    private func start(_ id: String) {
        tasks[id] = Task { [weak self] in await self?.upload(id) }
    }

    private func upload(_ id: String) async {
        guard let item = items.first(where: { $0.id == id }) else { return }
        do throws(APIError) {
            let uploaded = try await media.upload(item.file, name: item.name, as: item.purpose) { [weak self] stage in
                Task { @MainActor in self?.update(id, .uploading(stage)) }
            }
            update(id, .ready(uploaded.id))
        } catch {
            guard error.code != .cancelled else { return }
            update(id, .failed(error.message.isEmpty ? "The upload failed. Try again." : error.message))
        }
    }

    private func update(_ id: String, _ phase: PendingAttachment.Phase) {
        guard let index = items.firstIndex(where: { $0.id == id }) else { return }
        if case .ready = items[index].phase, case .uploading = phase { return }
        items[index].phase = phase
    }
}

struct SubscriptionBook: Equatable {
    private(set) var subscribed: Set<String> = []
    private(set) var watched: Set<String> = []
    private(set) var seen: [String: Int64] = [:]

    mutating func reset() {
        subscribed = []
        watched = []
    }

    mutating func saw(_ room: String, seq: Int64) {
        seen[room] = max(seen[room] ?? 0, seq)
    }

    mutating func keep(_ rooms: Set<String>) {
        subscribed.formIntersection(rooms)
    }

    mutating func drop(_ room: String) {
        subscribed.remove(room)
    }

    mutating func pending(rooms: [String], peers: Set<String>) -> [ChatCommand] {
        let newRooms = rooms.filter { !subscribed.contains($0) }
        let newPeers = peers.filter { !$0.isEmpty && !watched.contains($0) }.sorted()
        subscribed.formUnion(newRooms)
        watched.formUnion(newPeers)
        let subscriptions = newRooms.map { ChatCommand.subscribe(room: $0, since: seen[$0] ?? 0) }
        return subscriptions + newPeers.map { ChatCommand.watchPresence(user: $0) }
    }
}

@MainActor
@Observable
final class PeopleSearch {
    private(set) var results: [PublicProfile] = []
    private(set) var loading = false
    @ObservationIgnored private let auth: any AuthAPI
    @ObservationIgnored private let me: String
    @ObservationIgnored private let found: @MainActor ([PublicProfile]) -> Void
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored let delay: Duration

    init(
        auth: any AuthAPI,
        me: String,
        delay: Duration = .milliseconds(300),
        found: @escaping @MainActor ([PublicProfile]) -> Void = { _ in }
    ) {
        self.auth = auth
        self.me = me
        self.delay = delay
        self.found = found
    }

    func update(_ query: String) {
        task?.cancel()
        let needle = query.trimmingCharacters(in: .whitespaces)
        guard needle.count >= 2 else {
            results = []
            loading = false
            return
        }
        loading = true
        task = Task { [weak self, auth, me, delay] in
            try? await Task.sleep(for: delay)
            guard !Task.isCancelled else { return }
            let people = (try? await auth.searchUsers(needle)) ?? []
            guard !Task.isCancelled, let self else { return }
            results = people.filter { $0.id != me }
            loading = false
            found(results)
        }
    }
}
