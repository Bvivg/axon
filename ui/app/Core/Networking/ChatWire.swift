import Foundation
import SwiftProtobuf

enum ChatTarget: Hashable, Sendable {
    case room(String)
    case person(String)
}

struct OutgoingDraft: Equatable, Sendable {
    var clientID: String
    var target: ChatTarget
    var body: String
    var uploadIDs: [String] = []
    var replyTo: String?
    var forwardedFrom: String?
}

enum ChatCommand: Equatable, Sendable {
    case subscribe(room: String, since: Int64)
    case unsubscribe(room: String)
    case typing(room: String)
    case watchPresence(user: String)
    case send(OutgoingDraft)
    case edit(clientID: String, message: String, body: String)
    case delete([String])
    case call(CallFrameKind, CallSocketFrame)
}

enum CallFrameKind: String, Sendable {
    case attach = "call_attach"
    case heartbeat = "call_heartbeat"
    case resync = "call_resync"
    case ack = "call_ack"
    case signal = "call_signal"
}

enum ChatEvent: Equatable, Sendable {
    case state(ChannelState)
    case message(ChatMessage)
    case updated(ChatMessage)
    case subscribed(room: String, seq: Int64, behind: Bool)
    case ack(room: String, clientID: String, seq: Int64)
    case read(room: String, user: String, seq: Int64)
    case typing(room: String, user: String)
    case presence(user: String)
    case roomAdded(room: String, message: ChatMessage?)
    case roomRemoved(room: String)
    case failure(code: String, reason: String, clientID: String?)
    case call(CallSocketEvent)
    case revoked
}

enum ChatWire {
    static func encode(_ command: ChatCommand) -> String? {
        guard let data = try? JSONSerialization.data(withJSONObject: frame(command), options: [.sortedKeys]) else { return nil }
        return String(data: data, encoding: .utf8)
    }

    static func frame(_ command: ChatCommand) -> [String: Any] {
        switch command {
        case .subscribe(let room, let since):
            since > 0 ? ["type": "subscribe", "room_id": room, "since": since] : ["type": "subscribe", "room_id": room]
        case .unsubscribe(let room):
            ["type": "unsubscribe", "room_id": room]
        case .typing(let room):
            ["type": "typing", "room_id": room]
        case .watchPresence(let user):
            ["type": "watch_presence", "to_user_id": user]
        case .edit(let clientID, let message, let body):
            ["type": "edit", "client_id": clientID, "message_id": message, "body": body]
        case .delete(let ids):
            ids.count == 1 ? ["type": "delete", "message_id": ids[0]] : ["type": "delete", "message_ids": ids]
        case .send(let draft):
            sendFrame(draft)
        case .call(let kind, let call):
            ["type": kind.rawValue, "call": callObject(call)]
        }
    }

    private static func callObject(_ frame: CallSocketFrame) -> Any {
        guard let data = try? frame.jsonUTF8Data(),
              let object = try? JSONSerialization.jsonObject(with: data)
        else { return [String: Any]() }
        return object
    }

    static func callEvent(_ object: Any?) -> CallSocketEvent? {
        guard let object, JSONSerialization.isValidJSONObject(object),
              let data = try? JSONSerialization.data(withJSONObject: object)
        else { return nil }
        var options = JSONDecodingOptions()
        options.ignoreUnknownFields = true
        return try? CallSocketEvent(jsonUTF8Data: data, options: options)
    }

    private static func sendFrame(_ draft: OutgoingDraft) -> [String: Any] {
        var frame: [String: Any] = ["type": "send", "client_id": draft.clientID, "body": draft.body]
        switch draft.target {
        case .room(let id): frame["room_id"] = id
        case .person(let id): frame["to_user_id"] = id
        }
        if draft.uploadIDs.count == 1 {
            frame["upload_id"] = draft.uploadIDs[0]
        } else if !draft.uploadIDs.isEmpty {
            frame["upload_ids"] = draft.uploadIDs
        }
        if let reply = draft.replyTo { frame["reply_to_id"] = reply }
        if let origin = draft.forwardedFrom { frame["forwarded_from_id"] = origin }
        return frame
    }

    static func event(from text: String) -> ChatEvent? {
        guard let data = text.data(using: .utf8),
              let frame = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let type = frame["type"] as? String
        else { return nil }
        let message = (frame["message"] as? [String: Any]).flatMap(message(from:))
        switch type {
        case "message":
            return message.map(ChatEvent.message)
        case "message_updated":
            return message.map(ChatEvent.updated)
        case "room_added":
            return .roomAdded(room: frame["room_id"] as? String ?? "", message: message)
        case "call_event":
            return callEvent(frame["call"]).map(ChatEvent.call)
        default:
            return signal(type, frame)
        }
    }

    private static func signal(_ type: String, _ frame: [String: Any]) -> ChatEvent? {
        let room = frame["room_id"] as? String ?? ""
        let user = frame["user_id"] as? String ?? ""
        let seq = int64(frame["seq"])
        switch type {
        case "subscribed":
            return .subscribed(room: room, seq: seq, behind: frame["behind"] as? Bool ?? false)
        case "ack":
            return (frame["client_id"] as? String).map { .ack(room: room, clientID: $0, seq: seq) }
        case "read":
            return .read(room: room, user: user, seq: seq)
        case "typing":
            return .typing(room: room, user: user)
        case "presence":
            return .presence(user: user)
        case "room_removed":
            return .roomRemoved(room: room)
        case "error":
            return .failure(
                code: frame["code"] as? String ?? "",
                reason: frame["reason"] as? String ?? "",
                clientID: frame["client_id"] as? String
            )
        default:
            return nil
        }
    }

    static func message(from wire: [String: Any]) -> ChatMessage? {
        guard let id = wire["id"] as? String, let room = wire["room_id"] as? String else { return nil }
        var message = ChatMessage()
        message.id = id
        message.roomID = room
        message.authorID = wire["author_id"] as? String ?? ""
        message.body = wire["body"] as? String ?? ""
        message.seq = int64(wire["seq"])
        message.kind = kind(wire["kind"] as? String)
        if let sent = (wire["sent_at"] as? String).flatMap(timestamp) { message.sentAt = sent }
        if let client = wire["client_id"] as? String, !client.isEmpty { message.clientID = client }
        if let reply = wire["reply_to_id"] as? String, !reply.isEmpty { message.replyToID = reply }
        if let origin = wire["forwarded_from_id"] as? String, !origin.isEmpty { message.forwardedFromID = origin }
        if let author = wire["forwarded_from_author_id"] as? String, !author.isEmpty { message.forwardedFromAuthorID = author }
        if let edited = (wire["edited_at"] as? String).flatMap(timestamp) { message.editedAt = edited }
        if let deleted = (wire["deleted_at"] as? String).flatMap(timestamp) { message.deletedAt = deleted }
        if let quote = wire["reply_to"] as? [String: Any] {
            message.replyTo = Axon_Chat_V1_ReplyPreview.with {
                $0.id = quote["id"] as? String ?? ""
                $0.authorID = quote["author_id"] as? String ?? ""
                $0.kind = kind(quote["kind"] as? String)
                $0.body = quote["body"] as? String ?? ""
                $0.deleted = quote["deleted"] as? Bool ?? false
            }
        }
        if let payload = wire["payload"] as? [String: Any] {
            message.payload = self.payload(payload, kind: message.kind)
        }
        return message
    }

    static func kind(_ raw: String?) -> Axon_Chat_V1_MessageKind {
        switch raw {
        case "text": .text
        case "voice": .voice
        case "attachment": .attachment
        case "system": .system
        case "image": .image
        case "video": .video
        case "album": .album
        default: .text
        }
    }

    static func timestamp(_ raw: String) -> Google_Protobuf_Timestamp? {
        guard let match = raw.wholeMatch(of: /(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})/) else {
            return nil
        }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        guard let date = formatter.date(from: "\(match.1)\(match.3)") else { return nil }
        let digits = String((match.2 ?? "").prefix(9))
        let nanos = Int32(digits.padding(toLength: 9, withPad: "0", startingAt: 0)) ?? 0
        return Google_Protobuf_Timestamp(seconds: Int64(date.timeIntervalSince1970.rounded(.down)), nanos: nanos)
    }

    private static func payload(_ raw: [String: Any], kind: Axon_Chat_V1_MessageKind) -> ChatMessage.OneOf_Payload? {
        var raw = raw
        if var items = raw["items"] as? [[String: Any]] {
            for index in items.indices {
                items[index]["kind"] = protoKindName(items[index]["kind"] as? String)
            }
            raw["items"] = items
        }
        guard let data = try? JSONSerialization.data(withJSONObject: raw) else { return nil }
        var options = JSONDecodingOptions()
        options.ignoreUnknownFields = true
        switch kind {
        case .voice: return (try? Axon_Chat_V1_VoicePayload(jsonUTF8Data: data, options: options)).map { .voice($0) }
        case .attachment: return (try? Axon_Chat_V1_AttachmentPayload(jsonUTF8Data: data, options: options)).map { .attachment($0) }
        case .system: return (try? Axon_Chat_V1_SystemPayload(jsonUTF8Data: data, options: options)).map { .system($0) }
        case .image: return (try? Axon_Chat_V1_ImagePayload(jsonUTF8Data: data, options: options)).map { .image($0) }
        case .video: return (try? Axon_Chat_V1_VideoPayload(jsonUTF8Data: data, options: options)).map { .video($0) }
        case .album: return (try? Axon_Chat_V1_AlbumPayload(jsonUTF8Data: data, options: options)).map { .album($0) }
        default: return nil
        }
    }

    private static func protoKindName(_ raw: String?) -> String {
        switch kind(raw) {
        case .image: "MESSAGE_KIND_IMAGE"
        case .video: "MESSAGE_KIND_VIDEO"
        case .attachment: "MESSAGE_KIND_ATTACHMENT"
        case .voice: "MESSAGE_KIND_VOICE"
        default: "MESSAGE_KIND_UNSPECIFIED"
        }
    }

    private static func int64(_ value: Any?) -> Int64 {
        switch value {
        case let number as NSNumber: number.int64Value
        case let text as String: Int64(text) ?? 0
        default: 0
        }
    }
}
