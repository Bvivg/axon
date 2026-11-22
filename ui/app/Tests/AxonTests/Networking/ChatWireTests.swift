import Foundation
import Testing
@testable import Axon

struct ChatWireTests {
    private func object(_ text: String?) -> [String: Any] {
        guard let data = text?.data(using: .utf8) else { return [:] }
        return (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] ?? [:]
    }

    @Test func sendsOneUploadAsAnIdAndSeveralAsAnAlbum() {
        let single = OutgoingDraft(clientID: "c1", target: .room("r1"), body: "hi", uploadIDs: ["u1"], replyTo: "m0")
        let album = OutgoingDraft(clientID: "c2", target: .person("p1"), body: "", uploadIDs: ["u1", "u2"])
        let one = object(ChatWire.encode(.send(single)))
        let many = object(ChatWire.encode(.send(album)))
        #expect(one["type"] as? String == "send")
        #expect(one["room_id"] as? String == "r1")
        #expect(one["upload_id"] as? String == "u1")
        #expect(one["upload_ids"] == nil)
        #expect(one["reply_to_id"] as? String == "m0")
        #expect(many["to_user_id"] as? String == "p1")
        #expect(many["upload_ids"] as? [String] == ["u1", "u2"])
        #expect(many["room_id"] == nil)
    }

    @Test func encodesSubscriptionsDeletesAndEdits() {
        #expect(ChatWire.encode(.subscribe(room: "r1", since: 0)) == #"{"room_id":"r1","type":"subscribe"}"#)
        #expect(ChatWire.encode(.subscribe(room: "r1", since: 7)) == #"{"room_id":"r1","since":7,"type":"subscribe"}"#)
        #expect(ChatWire.encode(.delete(["m1"])) == #"{"message_id":"m1","type":"delete"}"#)
        #expect(object(ChatWire.encode(.delete(["m1", "m2"])))["message_ids"] as? [String] == ["m1", "m2"])
        let edit = ChatWire.encode(.edit(clientID: "c", message: "m", body: "b"))
        #expect(edit == #"{"body":"b","client_id":"c","message_id":"m","type":"edit"}"#)
        #expect(ChatWire.encode(.watchPresence(user: "u")) == #"{"to_user_id":"u","type":"watch_presence"}"#)
    }

    @Test func decodesAMessageWithAnAlbumReplyAndEdit() throws {
        let frame = """
        {"type":"message","message":{"id":"m1","room_id":"r1","author_id":"a1","body":"look","seq":12,
        "sent_at":"2026-11-21T10:00:00.123456789Z","client_id":"c1","kind":"album","edited_at":"2026-11-21T10:01:00Z",
        "reply_to_id":"m0","reply_to":{"id":"m0","author_id":"a2","kind":"image","body":"","deleted":true},
        "payload":{"items":[{"kind":"image","url":"u1","thumbnail_url":"t1","width":4,"height":3,"size_bytes":10,"mime":"image/jpeg"},
        {"kind":"attachment","url":"u2","filename":"a.pdf","width":0,"height":0,"size_bytes":20,"mime":"application/pdf"}]}}}
        """
        guard case .message(let message) = try #require(ChatWire.event(from: frame)) else {
            Issue.record("not a message")
            return
        }
        #expect(message.seq == 12)
        #expect(message.sentAt.seconds == 1_795_255_200)
        #expect(message.sentAt.nanos == 123_456_789)
        #expect(message.clientID == "c1")
        #expect(message.isEdited)
        #expect(message.replyTo.deleted)
        #expect(message.replyTo.kind == .image)
        guard case .album(let items) = message.content else {
            Issue.record("not an album")
            return
        }
        #expect(items.map(\.kind) == [.image, .attachment])
        #expect(items[1].filename == "a.pdf")
        #expect(MediaItem.items(of: message).map(\.id) == ["m1#0"])
    }

    @Test func decodesVoiceAndSystemPayloads() throws {
        let voice = try #require(ChatWire.event(from: """
        {"type":"message_updated","message":{"id":"m","room_id":"r","author_id":"a","body":"","seq":1,
        "sent_at":"2026-11-21T10:00:00+03:00","kind":"voice","payload":{"duration_ms":65000,"url":"v","mime":"audio/mp4"}}}
        """))
        guard case .updated(let message) = voice, case .voice(let payload) = message.content else {
            Issue.record("not a voice update")
            return
        }
        #expect(payload.durationMs == 65_000)
        #expect(message.sentAt.seconds == 1_795_244_400)
        let system = try #require(ChatWire.event(from: """
        {"type":"room_added","room_id":"r","message":{"id":"s","room_id":"r","author_id":"a","body":"","seq":1,
        "sent_at":"2026-11-21T10:00:00Z","kind":"system","payload":{"event":"member_added","actor_id":"a","target_ids":["me","b"]}}}
        """))
        guard case .roomAdded(let room, let added?) = system else {
            Issue.record("not a room")
            return
        }
        #expect(room == "r")
        #expect(added.systemText(me: "me") { $0 == "a" ? "Ada" : "Bob" } == "Ada added you and Bob")
    }

    @Test(arguments: [
        (#"{"type":"ack","room_id":"r","client_id":"c","seq":4}"#, ChatEvent.ack(room: "r", clientID: "c", seq: 4)),
        (#"{"type":"read","room_id":"r","user_id":"u","seq":9}"#, ChatEvent.read(room: "r", user: "u", seq: 9)),
        (#"{"type":"typing","room_id":"r","user_id":"u"}"#, ChatEvent.typing(room: "r", user: "u")),
        (#"{"type":"presence","user_id":"u"}"#, ChatEvent.presence(user: "u")),
        (#"{"type":"subscribed","room_id":"r","seq":3,"behind":true}"#, ChatEvent.subscribed(room: "r", seq: 3, behind: true)),
        (#"{"type":"room_removed","room_id":"r"}"#, ChatEvent.roomRemoved(room: "r")),
        (
            #"{"type":"error","code":"invalid","reason":"too long","client_id":"c"}"#,
            ChatEvent.failure(code: "invalid", reason: "too long", clientID: "c")
        ),
    ])
    func decodesSignals(frame: String, expected: ChatEvent) {
        #expect(ChatWire.event(from: frame) == expected)
    }

    @Test func ignoresUnknownAndBrokenFrames() {
        #expect(ChatWire.event(from: #"{"type":"call_offer"}"#) == nil)
        #expect(ChatWire.event(from: "nope") == nil)
        #expect(ChatWire.timestamp("2026-11-21 10:00") == nil)
    }

    @Test func mapsStoredFilesToTheMediaProxy() throws {
        let gateway = try GatewayAddress(parsing: "http://localhost:18080")
        let key = "0f0e0d0c-0b0a-0908-0706-050403020100/a1b2c3d4-0000-4000-8000-000000000001.jpg"
        #expect(gateway.media("http://minio:9000/chat-attachments/\(key)")?.absoluteString == "http://localhost:18080/api/media/\(key)")
        #expect(gateway.media("/chat-attachments/../etc/passwd") == nil)
        #expect(gateway.media("/api/media/\(key)")?.absoluteString == "http://localhost:18080/api/media/\(key)")
        #expect(gateway.sameOrigin(URL(string: "http://localhost:18080/api/media/x")!))
        #expect(!gateway.sameOrigin(URL(string: "https://cdn.example.com/a.png")!))
        #expect(gateway.media("https://cdn.example.com/a.png")?.absoluteString == "https://cdn.example.com/a.png")
        #expect(gateway.media("") == nil)
        #expect(gateway.chatSocketURL.absoluteString == "ws://localhost:18080/ws/chat")
        #expect(gateway.uploadsURL.absoluteString == "http://localhost:18080/api/uploads")
    }

    @Test func resumesOnlyTheMissingParts() {
        let session = UploadSession(id: "s", status: "uploading", partSize: 5, partCount: 4, parts: [1, 3], failure: nil, upload: nil)
        #expect(session.missing() == [2, 4])
        #expect(LiveMediaAPI.fraction(15, of: 20) == 0.75)
        #expect(UploadMessages.failure("too_large", .voice) == "That file is too large. Voice messages can be up to 10 MB.")
    }
}

@Suite(.timeLimit(.minutes(1)))
struct WebSocketChatSocketTests {
    let sleeper = RecordingSleeper()

    private func socket(_ transport: ScriptedSocketTransport, vault: TokenVault) -> WebSocketChatSocket {
        WebSocketChatSocket(
            url: URL(string: "ws://localhost/ws/chat")!,
            vault: vault,
            transport: transport,
            policy: ReconnectPolicy(jitter: 0),
            sleeper: sleeper
        )
    }

    @Test func opensWithTheChatProtocolAndRelaysFrames() async {
        let wire = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(wire))
        let socket = socket(transport, vault: Fixtures.vault(Fixtures.tokens("a-1")))
        var events = socket.events.makeAsyncIterator()
        #expect(await events.next() == .state(.connecting(attempt: 0)))
        #expect(await events.next() == .state(.open))
        #expect(transport.attempts.first?.first == "axon.chat.v1")
        #expect(transport.bearers == ["a-1"])
        #expect(await socket.send(.typing(room: "r")))
        #expect(wire.sent == [#"{"room_id":"r","type":"typing"}"#])
        wire.deliver(#"{"type":"presence","user_id":"u"}"#)
        #expect(await events.next() == .presence(user: "u"))
        await socket.close()
    }

    @Test func reconnectsAfterADropAndStopsWhenTheSessionIsRevoked() async {
        let first = ScriptedSocket()
        let second = ScriptedSocket()
        let socket = socket(ScriptedSocketTransport(.connect(first), .connect(second)), vault: Fixtures.vault(Fixtures.tokens("a-1")))
        var events = socket.events.makeAsyncIterator()
        _ = await events.next()
        _ = await events.next()
        first.drop(code: 1006)
        #expect(await events.next() == .state(.connecting(attempt: 1)))
        #expect(await events.next() == .state(.open))
        #expect(sleeper.delays == [.seconds(1)])
        second.drop(code: WebSocketChatSocket.sessionRevoked)
        #expect(await events.next() == .revoked)
        #expect(await events.next() == .state(.closed))
        #expect(await events.next() == nil)
    }

    @Test func renewsAnExpiredTokenOnce() async {
        let first = ScriptedSocket()
        let second = ScriptedSocket()
        let transport = ScriptedSocketTransport(.connect(first), .connect(second))
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2", refresh: "r-2")))
        let socket = socket(transport, vault: Fixtures.vault(Fixtures.tokens("a-1", refresh: "r-1"), refresher: refresher))
        var events = socket.events.makeAsyncIterator()
        _ = await events.next()
        _ = await events.next()
        first.drop(code: WebSocketChatSocket.tokenExpired)
        #expect(await events.next() == .state(.connecting(attempt: 0)))
        #expect(await events.next() == .state(.open))
        #expect(transport.bearers == ["a-1", "a-2"])
        #expect(sleeper.delays.isEmpty)
        await socket.close()
    }
}
