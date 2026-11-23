import Foundation
import Synchronization
@testable import Axon

struct Forwarded: Equatable {
    let messages: [String]
    let rooms: [String]
    let comment: String
}

final class FakeChat: ChatAPI, @unchecked Sendable {
    var result: Result<[Room], APIError> = .success([])
    var details: [String: RoomDetails] = [:]
    var pages: [MessagePage] = []
    var history: [HistoryQuery] = []
    var found: [ChatMessage] = []
    var reads: [Int64] = []
    var direct: Room?
    var created: Room?
    var forwarded: [Forwarded] = []
    var readerList: [MessageReader] = []
    var hidden: [String] = []
    var left: [String] = []
    var roomLoads = 0

    func rooms() async throws(APIError) -> [Room] {
        roomLoads += 1
        return try result.get()
    }

    func room(id: String) async throws(APIError) -> RoomDetails {
        details[id] ?? RoomDetails(room: Room.with { $0.id = id }, members: [])
    }

    func messages(in room: String, _ query: HistoryQuery) async throws(APIError) -> MessagePage {
        history.append(query)
        return pages.isEmpty ? MessagePage(messages: [], hasMore: false) : pages.removeFirst()
    }

    func search(in room: String, for text: String) async throws(APIError) -> [ChatMessage] { found }

    func markRead(room: String, seq: Int64) async throws(APIError) {
        reads.append(seq)
    }

    func directRoom(with user: String) async throws(APIError) -> Room? { direct }

    func createGroup(name: String, members: [String]) async throws(APIError) -> Room {
        guard let created else { throw APIError(.unavailable) }
        return created
    }

    func addMembers(_ users: [String], to room: String) async throws(APIError) {}

    func removeMember(_ user: String, from room: String) async throws(APIError) {}

    func renameGroup(_ room: String, to name: String) async throws(APIError) -> Room {
        Room.with {
            $0.id = room
            $0.name = name
        }
    }

    func leave(room: String) async throws(APIError) { left.append(room) }

    func hide(room: String) async throws(APIError) { hidden.append(room) }

    func readers(of message: String) async throws(APIError) -> [MessageReader] { readerList }

    func forward(_ messages: [String], to rooms: [String], comment: String, clientID: String) async throws(APIError) -> Int {
        forwarded.append(Forwarded(messages: messages, rooms: rooms, comment: comment))
        return rooms.count
    }
}

final class FakeChatSocket: ChatSocket, @unchecked Sendable {
    let events: AsyncStream<ChatEvent>
    private let feed: AsyncStream<ChatEvent>.Continuation
    private let log = Mutex<[ChatCommand]>([])
    private let open = Mutex(true)

    var sent: [ChatCommand] { log.withLock { $0 } }

    var accepting: Bool {
        get { open.withLock { $0 } }
        set { open.withLock { $0 = newValue } }
    }

    var drafts: [OutgoingDraft] {
        sent.compactMap { command in
            if case .send(let draft) = command { return draft }
            return nil
        }
    }

    var edits: [ChatCommand] {
        sent.filter { command in
            if case .edit = command { return true }
            return false
        }
    }

    init() {
        (events, feed) = AsyncStream.makeStream()
    }

    func emit(_ event: ChatEvent) {
        feed.yield(event)
    }

    func send(_ command: ChatCommand) async -> Bool {
        guard accepting else { return false }
        log.withLock { $0.append(command) }
        return true
    }

    func reconnect() async {}

    func close() async {
        feed.finish()
    }
}

final class FakeChatSockets: ChatSocketFactory, @unchecked Sendable {
    let socket = FakeChatSocket()

    func open() -> any ChatSocket { socket }
}

final class FakeMedia: MediaAPI, @unchecked Sendable {
    private let recorded = Mutex<[UploadPurpose]>([])

    var uploads: [UploadPurpose] { recorded.withLock { $0 } }
    var uploadResult: Result<UploadedFile, APIError> = .success(UploadedFile(id: "upload-1", kind: "image"))

    func link(_ raw: String) -> URL? { URL(string: raw) }

    func upload(
        _ file: URL,
        name: String,
        as purpose: UploadPurpose,
        progress: @escaping @Sendable (UploadStage) -> Void
    ) async throws(APIError) -> UploadedFile {
        recorded.withLock { $0.append(purpose) }
        progress(.uploading(1))
        return try uploadResult.get()
    }

    func data(_ link: URL) async throws(APIError) -> Data { Data() }

    func download(_ link: URL, named name: String) async throws(APIError) -> URL { link }

    func playbackHeaders(for link: URL) async -> [String: String] { [:] }

    var avatarResult: Result<AvatarUpload, APIError> = .failure(APIError(.unavailable, "not scripted"))

    func uploadAvatar(_ image: Data, type: String) async throws(APIError) -> AvatarUpload {
        try avatarResult.get()
    }
}
