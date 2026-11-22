import Foundation
import SwiftProtobuf
import Testing
@testable import Axon

enum ChatFixtures {
    static let room = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

    static func message(
        _ seq: Int64,
        author: String = GameFixtures.rival,
        room: String = room,
        body: String? = nil,
        at seconds: Int64? = nil,
        client: String? = nil
    ) -> ChatMessage {
        ChatMessage.with {
            $0.id = "m\(seq)"
            $0.roomID = room
            $0.authorID = author
            $0.body = body ?? "message \(seq)"
            $0.kind = .text
            $0.seq = seq
            $0.sentAt = Google_Protobuf_Timestamp(seconds: seconds ?? 1_795_255_200 + seq * 10)
            if let client { $0.clientID = client }
        }
    }
}

@MainActor
final class RecordingAlerts: ChatAlerting {
    var posted: [String] = []
    var activations = 0

    func activate() {
        activations += 1
    }

    func post(title: String, body: String, room: String) {
        posted.append("\(title): \(body)")
    }
}

struct ChatModelTests {
    @Test func mergesBySeqWithoutDuplicates() {
        var timeline = ChatTimeline()
        timeline.merge([ChatFixtures.message(3), ChatFixtures.message(1)])
        let added = timeline.merge([ChatFixtures.message(2), ChatFixtures.message(3)])
        #expect(added.map(\.seq) == [2])
        #expect(timeline.messages.map(\.seq) == [1, 2, 3])
        #expect(timeline.oldestSeq == 1)
        #expect(timeline.newestSeq == 3)
    }

    @Test func appliesUpdatesToRepliesToo() {
        var timeline = ChatTimeline()
        var reply = ChatFixtures.message(2)
        reply.replyToID = "m1"
        reply.replyTo = ConversationViewModel.quote(ChatFixtures.message(1))
        timeline.merge([ChatFixtures.message(1), reply])
        var deleted = ChatFixtures.message(1)
        deleted.deletedAt = Google_Protobuf_Timestamp(seconds: 5)
        let applied = timeline.apply(deleted)
        let missing = timeline.apply(ChatFixtures.message(9))
        #expect(applied)
        #expect(!missing)
        #expect(timeline.messages[0].content == .deleted)
        #expect(timeline.messages[1].replyTo.deleted)
    }

    @Test func groupsSeriesAndDays() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let rows = TimelineRow.rows(for: [
            ChatFixtures.message(1, at: 1_795_255_200),
            ChatFixtures.message(2, at: 1_795_255_260),
            ChatFixtures.message(3, author: GameFixtures.me, at: 1_795_255_300),
            ChatFixtures.message(4, author: GameFixtures.me, at: 1_795_341_600),
        ], calendar: calendar)
        let kinds = rows.map { row -> String in
            switch row.kind {
            case .day: "day"
            case .message(let message, let starts, let ends): "\(message.seq)\(starts ? "s" : "")\(ends ? "e" : "")"
            }
        }
        #expect(kinds == ["day", "1s", "2e", "3se", "day", "4se"])
    }

    @Test func forgetsTypingAfterItsLifetime() {
        var tracker = TypingTracker()
        let now = Date(timeIntervalSince1970: 100)
        tracker.start(room: "r", user: "b", now: now)
        tracker.start(room: "r", user: "a", now: now)
        #expect(tracker.typing(in: "r", now: now) == ["a", "b"])
        tracker.stop(room: "r", user: "a")
        #expect(tracker.typing(in: "r", now: now) == ["b"])
        tracker.prune(now: now.addingTimeInterval(TypingTracker.lifetime + 1))
        #expect(tracker.expiries.isEmpty)
        #expect(ChatFormat.typing(["Ada", "Bob", "Cy"]) == "3 people are typing…")
    }

    @Test func resubscribesFromTheLastSeenMessage() {
        var book = SubscriptionBook()
        let first = book.pending(rooms: ["r1", "r2"], peers: ["p"])
        let again = book.pending(rooms: ["r1", "r2"], peers: ["p"])
        book.saw("r1", seq: 7)
        book.saw("r1", seq: 3)
        book.reset()
        let resumed = book.pending(rooms: ["r1"], peers: [])
        #expect(first == [.subscribe(room: "r1", since: 0), .subscribe(room: "r2", since: 0), .watchPresence(user: "p")])
        #expect(again.isEmpty)
        #expect(resumed == [.subscribe(room: "r1", since: 7)])
    }
}

@MainActor
struct ChatsHubTests {
    private func hub(_ world: FakeWorld, alerts: RecordingAlerts? = nil) -> ChatsViewModel {
        ChatsViewModel(services: world.services, me: GameFixtures.me, alerts: alerts)
    }

    @Test func subscribesToEveryRoomAndWatchesPeersOnceOpen() async {
        let world = FakeWorld()
        world.chat.result = .success([
            SocialFixtures.room("r1", direct: GameFixtures.rival, sent: 10),
            SocialFixtures.room("r2", name: "Club", sent: 5),
        ])
        let model = hub(world)
        let run = Task { await model.run() }
        defer { run.cancel() }
        #expect(await eventually { model.loaded })
        world.sockets.socket.emit(.state(.open))
        #expect(await eventually { world.sockets.socket.sent.count == 3 })
        #expect(world.sockets.socket.sent == [
            .subscribe(room: "r1", since: 0), .subscribe(room: "r2", since: 0), .watchPresence(user: GameFixtures.rival),
        ])
    }

    @Test func countsUnreadOutsideTheOpenChatAndAlerts() async {
        let world = FakeWorld()
        world.chat.result = .success([
            SocialFixtures.room("r1", name: "One", sent: 10, seq: 1),
            SocialFixtures.room("r2", name: "Two", sent: 5, seq: 1),
        ])
        world.auth.profileMap = [GameFixtures.rival: GameFixtures.profile(GameFixtures.rival, "grace")]
        let alerts = RecordingAlerts()
        let model = hub(world, alerts: alerts)
        let run = Task { await model.run() }
        defer { run.cancel() }
        #expect(await eventually { model.loaded })
        model.activeRoom = "r1"
        world.sockets.socket.emit(.message(ChatFixtures.message(2, room: "r2", body: "hey")))
        world.sockets.socket.emit(.message(ChatFixtures.message(3, room: "r1")))
        #expect(await eventually { model.room("r1")?.lastMessage.seq == 3 })
        #expect(model.room("r2")?.unreadCount == 1)
        #expect(model.room("r1")?.unreadCount == 0)
        #expect(model.rooms.map(\.id) == ["r1", "r2"])
        #expect(await eventually { alerts.posted == ["grace · Two: hey"] })
        world.sockets.socket.emit(.read(room: "r2", user: GameFixtures.me, seq: 2))
        world.sockets.socket.emit(.read(room: "r1", user: GameFixtures.rival, seq: 3))
        #expect(await eventually { model.room("r2")?.unreadCount == 0 && model.room("r1")?.othersReadSeq == 3 })
    }

    @Test func showsTypingAndForgetsRemovedRooms() async {
        let world = FakeWorld()
        world.chat.result = .success([SocialFixtures.room("r1", direct: GameFixtures.rival, sent: 10)])
        let model = hub(world)
        let run = Task { await model.run() }
        defer { run.cancel() }
        #expect(await eventually { model.loaded })
        world.sockets.socket.emit(.typing(room: "r1", user: GameFixtures.rival))
        #expect(await eventually { model.typingText(in: model.rooms[0]) == "typing…" })
        world.sockets.socket.emit(.roomRemoved(room: "r1"))
        #expect(await eventually { model.rooms.isEmpty })
    }
}

struct OpenConversation {
    let model: ConversationViewModel
    let run: Task<Void, Never>
}

@MainActor
struct ConversationTests {
    private func open(_ world: FakeWorld, pages: [MessagePage]) async -> OpenConversation {
        world.chat.result = .success([SocialFixtures.room(ChatFixtures.room, direct: GameFixtures.rival, sent: 10)])
        world.chat.pages = pages
        let hub = ChatsViewModel(services: world.services, me: GameFixtures.me)
        let run = Task { await hub.run() }
        _ = await eventually { hub.loaded }
        world.sockets.socket.emit(.state(.open))
        _ = await eventually { hub.connected }
        let model = ConversationViewModel(target: .room(ChatFixtures.room), hub: hub, sendTimeout: .milliseconds(200))
        await model.start()
        return OpenConversation(model: model, run: run)
    }

    @Test func loadsTheNewestPageAndOlderHistoryFromTheTop() async {
        let world = FakeWorld()
        let opened = await open(world, pages: [
            MessagePage(messages: [ChatFixtures.message(51), ChatFixtures.message(52)], hasMore: true),
            MessagePage(messages: [ChatFixtures.message(50)], hasMore: false),
        ])
        let model = opened.model
        defer { opened.run.cancel() }
        #expect(model.timeline.messages.map(\.seq) == [51, 52])
        #expect(model.hasOlder)
        await model.loadOlder()
        #expect(world.chat.history.last?.before == 51)
        #expect(model.timeline.messages.map(\.seq) == [50, 51, 52])
        #expect(!model.hasOlder)
    }

    @Test func sendsThenConfirmsByAckAndEcho() async throws {
        let world = FakeWorld()
        let opened = await open(world, pages: [MessagePage(messages: [ChatFixtures.message(1)], hasMore: false)])
        let model = opened.model
        defer { opened.run.cancel() }
        model.reply(to: ChatFixtures.message(1))
        model.draft = "  hello  "
        await model.send()
        guard let draft = world.sockets.socket.drafts.last else {
            Issue.record("nothing sent")
            return
        }
        #expect(draft.body == "hello")
        #expect(draft.replyTo == "m1")
        #expect(draft.target == .room(ChatFixtures.room))
        #expect(model.draft.isEmpty)
        #expect(model.pendingOutgoing.map(\.status) == [.sending])
        world.sockets.socket.emit(.ack(room: ChatFixtures.room, clientID: draft.clientID, seq: 2))
        #expect(await eventually { model.outbox.items.first?.status == .sent })
        world.sockets.socket.emit(.message(ChatFixtures.message(2, author: GameFixtures.me, body: "hello", client: draft.clientID)))
        #expect(await eventually { model.outbox.items.isEmpty })
        #expect(model.timeline.messages.map(\.seq) == [1, 2])
    }

    @Test func marksUnconfirmedOnDropAndRetriesWithTheSameId() async {
        let world = FakeWorld()
        let opened = await open(world, pages: [])
        let model = opened.model
        defer { opened.run.cancel() }
        model.draft = "ping"
        await model.send()
        let first = model.outbox.items[0]
        world.sockets.socket.emit(.state(.connecting(attempt: 0)))
        #expect(await eventually { model.outbox.items.first?.status == .unconfirmed })
        world.sockets.socket.emit(.state(.open))
        await model.retry(model.outbox.items[0])
        #expect(world.sockets.socket.drafts.filter { $0.clientID == first.id }.count == 2)
        world.sockets.socket.emit(.failure(code: "invalid", reason: "Too long", clientID: first.id))
        #expect(await eventually { model.outbox.items.first?.status == .failed("Too long") })
        model.dismiss(model.outbox.items[0])
        #expect(model.outbox.items.isEmpty)
    }

    @Test func timesOutToUnconfirmed() async {
        let world = FakeWorld()
        let opened = await open(world, pages: [])
        let model = opened.model
        defer { opened.run.cancel() }
        model.draft = "ping"
        await model.send()
        #expect(await eventually { model.outbox.items.first?.status == .unconfirmed })
    }

    @Test func keepsTheDraftWhenOffline() async {
        let world = FakeWorld()
        let opened = await open(world, pages: [])
        let model = opened.model
        defer { opened.run.cancel() }
        world.sockets.socket.accepting = false
        model.draft = "ping"
        await model.send()
        #expect(model.draft == "ping")
        #expect(model.outbox.items.isEmpty)
        #expect(model.notice == "You're offline. The message was not sent.")
    }

    @Test func sendsUploadedAttachmentsAsOneMessage() async throws {
        let world = FakeWorld()
        let opened = await open(world, pages: [])
        let model = opened.model
        defer { opened.run.cancel() }
        let file = FileManager.default.temporaryDirectory.appending(path: "\(UUID().uuidString).jpg")
        try Data([1, 2, 3]).write(to: file)
        model.attach([file, file], as: .media)
        #expect(await eventually { model.tray.uploadedIDs.count == 2 })
        #expect(model.canSend)
        await model.send()
        guard let draft = world.sockets.socket.drafts.last else {
            Issue.record("nothing sent")
            return
        }
        #expect(draft.uploadIDs == ["upload-1", "upload-1"])
        #expect(world.media.uploads == [.media, .media])
        #expect(model.tray.isEmpty)
    }

    @Test func editsAndDeletesOnlyOwnMessages() async {
        let world = FakeWorld()
        let mine = ChatFixtures.message(2, author: GameFixtures.me, body: "draft")
        let opened = await open(world, pages: [MessagePage(messages: [ChatFixtures.message(1), mine], hasMore: false)])
        let model = opened.model
        defer { opened.run.cancel() }
        model.startEdit(ChatFixtures.message(1))
        #expect(model.editing == nil)
        model.startEdit(mine)
        #expect(model.draft == "draft")
        #expect(!model.canSend)
        model.draft = "final"
        await model.send()
        guard case .edit(_, let id, let body)? = world.sockets.socket.edits.last else {
            Issue.record("no edit")
            return
        }
        #expect(id == "m2")
        #expect(body == "final")
        #expect(model.editing == nil)
        await model.delete(["m1", "m2"])
        #expect(world.sockets.socket.sent.contains(.delete(["m2"])))
        var edited = mine
        edited.body = "final"
        edited.editedAt = Google_Protobuf_Timestamp(seconds: 9)
        world.sockets.socket.emit(.updated(edited))
        #expect(await eventually { model.message(id: "m2")?.isEdited == true })
    }

    @Test func startsADirectChatAndAdoptsTheRoomFromTheAck() async {
        let world = FakeWorld()
        world.chat.result = .success([])
        let hub = ChatsViewModel(services: world.services, me: GameFixtures.me)
        let run = Task { await hub.run() }
        defer { run.cancel() }
        _ = await eventually { hub.loaded }
        world.sockets.socket.emit(.state(.open))
        _ = await eventually { hub.connected }
        let model = ConversationViewModel(target: .person(GameFixtures.third), hub: hub)
        await model.start()
        #expect(model.roomID == nil)
        model.draft = "hi"
        await model.send()
        guard let draft = world.sockets.socket.drafts.last else {
            Issue.record("nothing sent")
            return
        }
        #expect(draft.target == .person(GameFixtures.third))
        let stored = ChatFixtures.message(1, author: GameFixtures.me, room: "new-room", body: "hi", client: draft.clientID)
        world.chat.pages = [MessagePage(messages: [stored], hasMore: false)]
        world.sockets.socket.emit(.ack(room: "new-room", clientID: draft.clientID, seq: 1))
        #expect(await eventually { model.roomID == "new-room" })
        #expect(await eventually { model.outbox.items.isEmpty })
        var edited = stored
        edited.clientID = ""
        edited.body = "hi there"
        edited.editedAt = Google_Protobuf_Timestamp(seconds: 9)
        world.sockets.socket.emit(.updated(edited))
        #expect(await eventually { model.message(id: "m1")?.body == "hi there" })
        #expect(model.message(id: "m1")?.clientID == draft.clientID)
        #expect(model.pendingOutgoing.isEmpty)
    }

    @Test func jumpsToAnOlderSearchResult() async {
        let world = FakeWorld()
        let opened = await open(world, pages: [
            MessagePage(messages: [ChatFixtures.message(60)], hasMore: true),
            MessagePage(messages: [ChatFixtures.message(59)], hasMore: true),
            MessagePage(messages: [ChatFixtures.message(58)], hasMore: false),
        ])
        let model = opened.model
        defer { opened.run.cancel() }
        await model.jump(to: ChatFixtures.message(58))
        #expect(model.scrollTarget == "m58")
        #expect(model.highlighted == "m58")
        #expect(model.timeline.messages.map(\.seq) == [58, 59, 60])
    }
}
