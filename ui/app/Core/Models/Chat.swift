import Foundation
import SwiftProtobuf

typealias RoomMember = Axon_Chat_V1_Member
typealias MessageReader = Axon_Chat_V1_MessageReader
typealias AlbumItem = Axon_Chat_V1_AlbumItem

enum MessageContent: Equatable {
    case text
    case voice(Axon_Chat_V1_VoicePayload)
    case image(Axon_Chat_V1_ImagePayload)
    case video(Axon_Chat_V1_VideoPayload)
    case file(Axon_Chat_V1_AttachmentPayload)
    case album([AlbumItem])
    case system(Axon_Chat_V1_SystemPayload)
    case deleted
}

extension ChatMessage {
    var content: MessageContent {
        if isDeleted { return .deleted }
        switch payload {
        case .voice(let voice): return .voice(voice)
        case .image(let image): return .image(image)
        case .video(let video): return .video(video)
        case .attachment(let file): return .file(file)
        case .album(let album): return .album(album.items)
        case .system(let system): return .system(system)
        case nil: return kind == .system ? .system(Axon_Chat_V1_SystemPayload()) : .text
        }
    }

    var isSystem: Bool { kind == .system }

    var isEdited: Bool { hasEditedAt && !isDeleted }

    var isForwarded: Bool { hasForwardedFromID && !forwardedFromID.isEmpty }

    var isEditable: Bool {
        guard !isDeleted, !isForwarded else { return false }
        switch content {
        case .text, .image, .video, .file, .album: return true
        default: return false
        }
    }

    var canSelect: Bool { !isSystem && !isDeleted }

    var sentDate: Date { sentAt.date }

    var caption: String { body.trimmingCharacters(in: .whitespacesAndNewlines) }

    var copyableText: String? {
        guard !isDeleted, !isSystem, !caption.isEmpty else { return nil }
        return body
    }

    var involvedUsers: [String] {
        guard case .system(let system) = payload else { return [authorID] }
        var ids = [authorID] + system.targetIds
        if system.hasActorID { ids.append(system.actorID) }
        if system.hasTargetID { ids.append(system.targetID) }
        return ids
    }
}

extension Room {
    var involvedUsers: [String] {
        (isDirect ? [peerUserID] : []) + (hasLastMessage ? lastMessage.involvedUsers : [])
    }
}

extension Axon_Chat_V1_ReplyPreview {
    var summary: String {
        if deleted { return "Message deleted" }
        let text = body.trimmingCharacters(in: .whitespacesAndNewlines)
        switch kind {
        case .image: return text.isEmpty ? "📷 Photo" : "📷 \(text)"
        case .video: return text.isEmpty ? "🎬 Video" : "🎬 \(text)"
        case .voice: return "🎤 Voice message"
        case .attachment: return text.isEmpty ? "📎 File" : "📎 \(text)"
        case .album: return text.isEmpty ? "🖼 Album" : "🖼 \(text)"
        default: return text
        }
    }
}

enum DeliveryStatus: Equatable, Sendable {
    case sending
    case sent
    case unconfirmed
    case failed(String)
}

struct OutgoingMessage: Identifiable, Equatable, Sendable {
    var draft: OutgoingDraft
    var status: DeliveryStatus
    var createdAt: Date
    var seq: Int64?
    var replyPreview: Axon_Chat_V1_ReplyPreview?

    var id: String { draft.clientID }
}

struct ChatTimeline: Equatable {
    private(set) var messages: [ChatMessage] = []

    var newestSeq: Int64 { messages.last?.seq ?? 0 }
    var oldestSeq: Int64? { messages.first?.seq }
    var isEmpty: Bool { messages.isEmpty }

    @discardableResult
    mutating func merge(_ incoming: [ChatMessage]) -> [ChatMessage] {
        let known = Set(messages.map(\.seq))
        var added: [ChatMessage] = []
        var seen = Set<Int64>()
        for message in incoming where !known.contains(message.seq) && seen.insert(message.seq).inserted {
            added.append(message)
        }
        guard !added.isEmpty else { return [] }
        messages = (messages + added).sorted { $0.seq < $1.seq }
        return added
    }

    mutating func replace(with page: [ChatMessage]) {
        let newest = page.map(\.seq).max() ?? 0
        let newer = messages.filter { $0.seq > newest }
        messages = []
        merge(page + newer)
    }

    @discardableResult
    mutating func refresh(with page: [ChatMessage]) -> Bool {
        var changed = !merge(page).isEmpty
        for message in page {
            guard let current = self.message(id: message.id) else { continue }
            var incoming = message
            if !incoming.hasClientID, current.hasClientID { incoming.clientID = current.clientID }
            if incoming != current { changed = apply(message) || changed }
        }
        return changed
    }

    @discardableResult
    mutating func apply(_ update: ChatMessage) -> Bool {
        guard let index = messages.firstIndex(where: { $0.id == update.id }) else { return false }
        var merged = update
        if merged.clientID.isEmpty, !messages[index].clientID.isEmpty { merged.clientID = messages[index].clientID }
        messages[index] = merged
        for position in messages.indices where messages[position].replyToID == update.id && messages[position].hasReplyTo {
            messages[position].replyTo.deleted = update.isDeleted
            messages[position].replyTo.body = update.isDeleted ? "" : update.body
        }
        return true
    }

    func message(id: String) -> ChatMessage? {
        messages.first { $0.id == id }
    }

    func contains(clientID: String) -> Bool {
        messages.contains { $0.hasClientID && $0.clientID == clientID }
    }
}

struct TimelineRow: Identifiable, Equatable {
    enum Kind: Equatable {
        case day(Date)
        case message(ChatMessage, startsSeries: Bool, endsSeries: Bool)
    }

    let id: String
    let kind: Kind

    static let seriesGap: TimeInterval = 5 * 60

    static func rows(for messages: [ChatMessage], calendar: Calendar = .current) -> [TimelineRow] {
        var rows: [TimelineRow] = []
        rows.reserveCapacity(messages.count + 8)
        for (index, message) in messages.enumerated() {
            let previous = index > 0 ? messages[index - 1] : nil
            let next = index + 1 < messages.count ? messages[index + 1] : nil
            if previous.map({ !calendar.isDate($0.sentDate, inSameDayAs: message.sentDate) }) ?? true {
                let day = calendar.startOfDay(for: message.sentDate)
                rows.append(TimelineRow(id: "day-\(Int(day.timeIntervalSince1970))", kind: .day(day)))
            }
            rows.append(TimelineRow(
                id: message.id,
                kind: .message(
                    message,
                    startsSeries: !continues(previous, message, calendar: calendar),
                    endsSeries: !continues(message, next, calendar: calendar)
                )
            ))
        }
        return rows
    }

    private static func continues(_ first: ChatMessage?, _ second: ChatMessage?, calendar: Calendar) -> Bool {
        guard let first, let second, !first.isSystem, !second.isSystem, first.authorID == second.authorID else { return false }
        guard calendar.isDate(first.sentDate, inSameDayAs: second.sentDate) else { return false }
        return second.sentDate.timeIntervalSince(first.sentDate) < seriesGap
    }
}

struct TypingTracker: Equatable {
    static let lifetime: TimeInterval = 6

    private(set) var expiries: [String: [String: Date]] = [:]

    mutating func start(room: String, user: String, now: Date = .now) {
        expiries[room, default: [:]][user] = now.addingTimeInterval(Self.lifetime)
    }

    mutating func stop(room: String, user: String) {
        expiries[room]?[user] = nil
        if expiries[room]?.isEmpty == true { expiries[room] = nil }
    }

    mutating func prune(now: Date = .now) {
        for (room, users) in expiries {
            let alive = users.filter { $0.value > now }
            expiries[room] = alive.isEmpty ? nil : alive
        }
    }

    func typing(in room: String, now: Date = .now) -> [String] {
        (expiries[room] ?? [:]).filter { $0.value > now }.keys.sorted()
    }
}

enum ChatFormat {
    static func dayLabel(_ day: Date, now: Date = .now, calendar: Calendar = .current) -> String {
        if calendar.isDateInToday(day) { return "Today" }
        if calendar.isDateInYesterday(day) { return "Yesterday" }
        if calendar.isDate(day, equalTo: now, toGranularity: .year) {
            return day.formatted(.dateTime.weekday(.wide).day().month(.wide))
        }
        return day.formatted(.dateTime.day().month(.wide).year())
    }

    static func size(_ bytes: Int64) -> String {
        ByteCountFormatter.string(fromByteCount: bytes, countStyle: .file)
    }

    static func lastSeen(_ profile: PublicProfile?, now: Date = .now) -> String? {
        guard let profile else { return nil }
        if profile.online { return "online" }
        guard profile.hasLastSeenAt else { return nil }
        let seen = profile.lastSeenAt.date
        let minutes = Int(now.timeIntervalSince(seen) / 60)
        if minutes < 1 { return "last seen just now" }
        if minutes < 60 { return "last seen \(minutes) min ago" }
        if Calendar.current.isDateInToday(seen) { return "last seen at \(seen.formatted(date: .omitted, time: .shortened))" }
        if Calendar.current.isDateInYesterday(seen) { return "last seen yesterday" }
        return "last seen \(seen.formatted(.dateTime.day().month(.abbreviated)))"
    }

    static func typing(_ names: [String]) -> String? {
        switch names.count {
        case 0: nil
        case 1: "\(names[0]) is typing…"
        case 2: "\(names[0]) and \(names[1]) are typing…"
        default: "\(names.count) people are typing…"
        }
    }
}

extension ChatMessage {
    func systemText(me: String, name: (String) -> String) -> String {
        guard case .system(let system) = payload else { return caption.isEmpty ? "The chat changed" : caption }
        let actor = system.hasActorID ? name(system.actorID) : "Someone"
        var ids = system.targetIds
        if ids.isEmpty, system.hasTargetID { ids = [system.targetID] }
        let targets = ids.map { $0 == me ? "you" : name($0) }
        let target = targets.count > 1
            ? targets.dropLast().joined(separator: ", ") + " and " + (targets.last ?? "")
            : targets.first ?? "someone"
        switch system.event {
        case "group_created": return "\(actor) created the group “\(caption)”"
        case "member_added": return "\(actor) added \(target)"
        case "member_removed": return "\(actor) removed \(target)"
        case "member_left": return "\(actor) left the group"
        case "group_renamed": return "\(actor) renamed the group to “\(caption)”"
        default: return caption.isEmpty ? "The chat changed" : caption
        }
    }
}

extension Axon_Chat_V1_Message: Identifiable {}
