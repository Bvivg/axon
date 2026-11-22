import Foundation

typealias Room = Axon_Chat_V1_Room
typealias RoomKind = Axon_Chat_V1_RoomKind
typealias ChatMessage = Axon_Chat_V1_Message
typealias Call = Axon_Calls_V1_Call
typealias Rating = Axon_Game_V1_Rating
typealias SignInSession = Axon_Auth_V1_Session

struct Account: Equatable, Sendable {
    var user: User
    var linked: Set<OAuthProvider>
}

struct ProfileChanges: Equatable, Sendable {
    var email: String?
    var username: String?
    var firstName: String?
    var lastName: String?
    var nickname: String?

    var isEmpty: Bool {
        email == nil && username == nil && firstName == nil && lastName == nil && nickname == nil
    }
}

extension Rating {
    static let starting: UInt32 = 1200

    var record: String {
        "\(games) rated · \(wins)W \(losses)L \(draws)D"
    }
}

extension Room {
    var isDirect: Bool { kind == .direct }

    func lastActivity() -> Date? {
        hasLastMessage ? lastMessage.sentAt.date : nil
    }
}

extension ChatMessage {
    var isDeleted: Bool { hasDeletedAt }

    var preview: String {
        if isDeleted { return "Message deleted" }
        let caption = body.trimmingCharacters(in: .whitespacesAndNewlines)
        switch payload {
        case .image:
            return caption.isEmpty ? "📷 Photo" : "📷 \(caption)"
        case .video:
            return caption.isEmpty ? "🎬 Video" : "🎬 \(caption)"
        case .voice(let voice):
            return "🎤 Voice message · \(Clock.duration(milliseconds: voice.durationMs))"
        case .attachment(let file):
            return "📎 \(file.filename)"
        case .album(let album):
            if album.items.contains(where: { $0.kind == .attachment }) {
                return caption.isEmpty ? "📎 Attachments · \(album.items.count)" : "📎 \(caption)"
            }
            return caption.isEmpty ? "🖼 Album · \(album.items.count)" : "🖼 \(caption)"
        case .system(let system):
            return Self.summary(of: system, name: caption)
        case nil:
            return body
        }
    }

    private static func summary(of system: Axon_Chat_V1_SystemPayload, name: String) -> String {
        switch system.event {
        case "group_created": "Group created"
        case "member_added": system.targetIds.count > 1 ? "New members joined" : "New member joined"
        case "member_removed": "A member was removed"
        case "member_left": "A member left"
        case "group_renamed": "Renamed to “\(name)”"
        default: name.isEmpty ? "The chat changed" : name
        }
    }
}

enum CallDirection: Equatable, Sendable {
    case incoming
    case outgoing
    case missed
}

extension Call {
    var isGroup: Bool { hasKind && kind == "group" }

    var isVideo: Bool { mode == .video }

    func direction(for me: String) -> CallDirection {
        if callerID == me { return .outgoing }
        return endReason == "missed" ? .missed : .incoming
    }

    func peer(for me: String) -> String? {
        participants.first { $0.userID != me }?.userID ?? (callerID == me ? nil : callerID)
    }

    func outcome(for me: String) -> String {
        guard state == .ended else { return "In progress" }
        if endReason == "missed" { return callerID == me ? "No answer" : "Missed" }
        return Self.outcomes[endReason] ?? "Failed"
    }

    var duration: Duration? {
        guard hasStartedAt, hasEndedAt else { return nil }
        return .seconds(max(0, endedAt.seconds - startedAt.seconds))
    }

    private static let outcomes: [String: String] = [
        "declined": "Declined",
        "cancelled": "Cancelled",
        "busy": "Busy",
        "ended": "Completed",
        "setup_timeout": "Failed",
        "connection_failed": "Failed",
        "reconnect_timeout": "Disconnected",
        "endpoint_expired": "Disconnected",
        "replaced_by_group": "Continued as a group call",
        "left": "Left call",
        "removed": "Removed",
        "participants_left": "Completed",
        "media_unavailable": "Media unavailable",
        "cleanup_timeout": "Cleanup timed out",
    ]
}

extension SignInSession {
    var device: String {
        let agent = userAgent
        guard !agent.isEmpty else { return "Unknown device" }
        if agent.hasPrefix("Axon/") { return Self.system(of: agent).map { "Axon app on \($0)" } ?? "Axon app" }
        let browser = if agent.contains("Edg/") {
            "Edge"
        } else if agent.contains("Chrome/") {
            "Chrome"
        } else if agent.contains("Firefox/") {
            "Firefox"
        } else if agent.contains("Safari/") {
            "Safari"
        } else {
            "Browser"
        }
        return Self.system(of: agent).map { "\(browser) on \($0)" } ?? browser
    }

    var isMobile: Bool {
        ["Android", "iPhone", "iPad", "iOS"].contains { userAgent.contains($0) }
    }

    private static func system(of agent: String) -> String? {
        if agent.contains("Windows") { return "Windows" }
        if agent.contains("iPhone") || agent.contains("iPad") || agent.contains("iOS") { return "iOS" }
        if agent.contains("Mac OS X") || agent.contains("macOS") { return "macOS" }
        if agent.contains("Android") { return "Android" }
        if agent.contains("Linux") { return "Linux" }
        return nil
    }
}

extension User {
    var personName: String {
        let full = [firstName, lastName].map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }.joined(separator: " ")
        if !full.isEmpty { return full }
        return shownName
    }

    var handle: String {
        hasUsername && !username.isEmpty ? "@\(username)" : email
    }

    var avatarLink: URL? {
        let link = hasAvatarUrls && !avatarUrls.medium.isEmpty ? avatarUrls.medium : avatarURL
        return link.isEmpty ? nil : URL(string: link)
    }
}

extension PublicProfile {
    var shownName: String {
        if hasDisplayName, !displayName.isEmpty { return displayName }
        if hasUsername, !username.isEmpty { return username }
        return "Player"
    }

    var avatarLink: URL? {
        let link = hasAvatarUrls && !avatarUrls.small.isEmpty ? avatarUrls.small : avatarURL
        return link.isEmpty ? nil : URL(string: link)
    }
}

enum Clock {
    static func duration(milliseconds: Int64) -> String {
        duration(.milliseconds(milliseconds))
    }

    static func duration(_ value: Duration) -> String {
        let total = Int(value.components.seconds)
        let hours = total / 3600
        let minutes = total % 3600 / 60
        let seconds = total % 60
        if hours > 0 { return String(format: "%d:%02d:%02d", hours, minutes, seconds) }
        return String(format: "%d:%02d", minutes, seconds)
    }

    static func short(_ date: Date?, now: Date = .now, calendar: Calendar = .current) -> String {
        guard let date else { return "" }
        if calendar.isDate(date, inSameDayAs: now) {
            return date.formatted(date: .omitted, time: .shortened)
        }
        return date.formatted(.dateTime.day(.twoDigits).month(.twoDigits))
    }
}
