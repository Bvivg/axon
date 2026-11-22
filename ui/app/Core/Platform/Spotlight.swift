import CoreSpotlight
import UniformTypeIdentifiers

struct SpotlightEntry: Hashable, Sendable {
    let identifier: String
    let title: String
    let detail: String
    let keywords: [String]
}

enum Spotlight {
    static let domain = "dev.axon.content"

    static func games(_ sessions: [Session], me: String, name: (Session) -> String) -> [SpotlightEntry] {
        sessions.map { session in
            let prefix = session.isFinished ? "replay" : "game"
            let headline = SessionStatusText.headline(session, me: me)
            let day = session.isFinished && session.hasUpdatedAt
                ? session.updatedAt.date.formatted(.dateTime.day().month(.abbreviated).hour().minute())
                : nil
            return SpotlightEntry(
                identifier: "\(prefix):\(session.id)",
                title: "\(session.kind.title) · \(name(session))",
                detail: day.map { "\(headline) · \($0)" } ?? headline,
                keywords: [session.kind.title, "Axon", "game"]
            )
        }
    }

    static func rooms(_ rooms: [Room], title: (Room) -> String) -> [SpotlightEntry] {
        rooms.map { room in
            SpotlightEntry(
                identifier: "room:\(room.id)",
                title: title(room),
                detail: room.isDirect ? "Chat" : "Group chat",
                keywords: ["Axon", "chat"]
            )
        }
    }

    static func index(_ entries: [SpotlightEntry]) {
        CSSearchableIndex.default().deleteSearchableItems(withDomainIdentifiers: [domain]) { _ in
            CSSearchableIndex.default().indexSearchableItems(entries.map(item)) { _ in }
        }
    }

    private static func item(_ entry: SpotlightEntry) -> CSSearchableItem {
        let attributes = CSSearchableItemAttributeSet(contentType: .content)
        attributes.title = entry.title
        attributes.contentDescription = entry.detail
        attributes.keywords = entry.keywords
        return CSSearchableItem(uniqueIdentifier: entry.identifier, domainIdentifier: domain, attributeSet: attributes)
    }

    static func clear() {
        CSSearchableIndex.default().deleteSearchableItems(withDomainIdentifiers: [domain]) { _ in }
    }
}
