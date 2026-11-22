import Foundation

enum AppLink: Hashable, Sendable {
    case open(AppTab, [Route])
    case quickMatch(GameKind)
    case newChat
    case palette

    static let scheme = "axon"

    init?(url: URL) {
        guard url.scheme == Self.scheme, let host = url.host() else { return nil }
        if let link = Self.plain[host] {
            self = link
            return
        }
        guard let argument = url.pathComponents.first(where: { $0 != "/" }) else { return nil }
        if let game = GameKind(rawValue: argument), let link = Self.game(host, game) {
            self = link
        } else if let link = Self.item(host, argument) {
            self = link
        } else {
            return nil
        }
    }

    private static let plain: [String: AppLink] = [
        "games": .open(.games, []),
        "calls": .open(.calls, []),
        "chats": .open(.chats, []),
        "profile": .open(.profile, []),
        "new-chat": .newChat,
        "search": .palette,
        "tournaments": .open(.games, [.tournaments]),
    ]

    private static func game(_ host: String, _ game: GameKind) -> AppLink? {
        switch host {
        case "lobby": .open(.games, [.lobby(game)])
        case "quick": .quickMatch(game)
        case "practice": .open(.games, [.lobby(game), .practice(game)])
        default: nil
        }
    }

    private static func item(_ host: String, _ id: String) -> AppLink? {
        switch host {
        case "game": .open(.games, [.game(id)])
        case "replay": .open(.games, [.replay(id)])
        case "tournament": .open(.games, [.tournaments, .tournament(id)])
        case "chat": .open(.chats, [.room(id)])
        default: nil
        }
    }

    var url: URL? {
        switch self {
        case .open(let tab, let routes):
            guard let route = routes.last else { return URL(string: "\(Self.scheme)://\(tab.rawValue)") }
            return route.link
        case .quickMatch(let game): return URL(string: "\(Self.scheme)://quick/\(game.rawValue)")
        case .newChat: return URL(string: "\(Self.scheme)://new-chat")
        case .palette: return URL(string: "\(Self.scheme)://search")
        }
    }

    static func searchable(_ identifier: String) -> AppLink? {
        let parts = identifier.split(separator: ":", maxSplits: 1).map(String.init)
        guard parts.count == 2 else { return nil }
        switch parts[0] {
        case "game": return .open(.games, [.game(parts[1])])
        case "replay": return .open(.games, [.replay(parts[1])])
        case "room": return .open(.chats, [.room(parts[1])])
        default: return nil
        }
    }
}

extension Route {
    var link: URL? {
        let path: String? = switch self {
        case .lobby(let game): "lobby/\(game.rawValue)"
        case .game(let id): "game/\(id)"
        case .replay(let id): "replay/\(id)"
        case .practice(let game): "practice/\(game.rawValue)"
        case .tournaments: "tournaments"
        case .tournament(let id): "tournament/\(id)"
        case .room(let id): "chat/\(id)"
        default: nil
        }
        return path.flatMap { URL(string: "\(AppLink.scheme)://\($0)") }
    }
}
