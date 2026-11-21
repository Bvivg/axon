import Foundation
import SwiftProtobuf

struct SessionCodec: Sendable {
    static var decodingOptions: JSONDecodingOptions {
        var options = JSONDecodingOptions()
        options.ignoreUnknownFields = true
        return options
    }

    static func subscribe(_ subscription: GameSubscription) -> SessionCommand {
        var command = SessionCommand()
        switch subscription {
        case .session(let id): command.subscribe = id
        case .match(let game): command.subscribeMatch = game.rawValue
        }
        return command
    }

    func encode(_ command: SessionCommand) throws -> String {
        try command.jsonString()
    }

    func decode(_ text: String) throws -> SessionEvent {
        try SessionEvent(jsonString: text, options: Self.decodingOptions)
    }

    func event(from text: String) -> ChannelEvent? {
        guard let decoded = try? decode(text) else { return nil }
        switch decoded.update {
        case .session(let session): return .session(session)
        case .error(let error): return .error(error)
        case .match(let ticket): return .match(ticket)
        case nil: return nil
        }
    }
}
