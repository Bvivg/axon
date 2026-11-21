import Foundation

enum AccessibilityID {
    enum Launch {
        static let gatewayArgument = "-gatewayBaseURL"
        static let resetArgument = "-uiTestReset"
    }

    enum Settings {
        static let open = "settings.open"
        static let gatewayURL = "settings.gatewayURL"
        static let save = "settings.save"
        static let error = "settings.error"
    }

    enum Auth {
        static let mode = "auth.mode"
        static let email = "auth.email"
        static let username = "auth.username"
        static let password = "auth.password"
        static let submit = "auth.submit"
        static let error = "auth.error"
        static let logout = "auth.logout"
    }

    enum Lobby {
        static let root = "lobby"
        static let joinID = "lobby.joinID"
        static let join = "lobby.join"
        static let cancelMatch = "lobby.cancelMatch"
        static let error = "lobby.error"

        static func create(_ game: String) -> String { "lobby.create.\(game)" }
        static func quickMatch(_ game: String) -> String { "lobby.quickMatch.\(game)" }
        static func session(_ id: String) -> String { "lobby.session.\(id)" }
    }

    enum Game {
        static let sessionID = "game.sessionID"
        static let status = "game.status"
        static let error = "game.error"
        static let spectators = "game.spectators"
        static let resign = "game.resign"
        static let offerDraw = "game.offerDraw"
        static let acceptDraw = "game.acceptDraw"
        static let declineDraw = "game.declineDraw"
        static let undo = "game.undo"
        static let replay = "game.replay"

        static func clock(_ seat: Int) -> String { "game.clock.\(seat)" }
        static func ticTacToeCell(_ index: Int) -> String { "ttt.cell.\(index)" }
        static func chessSquare(_ square: String) -> String { "chess.square.\(square)" }
        static func quoridorCell(_ cell: String) -> String { "quoridor.cell.\(cell)" }
        static func quoridorWall(_ wall: String) -> String { "quoridor.wall.\(wall)" }
    }

    enum Replay {
        static let slider = "replay.slider"
        static let first = "replay.first"
        static let previous = "replay.previous"
        static let next = "replay.next"
        static let last = "replay.last"
        static let position = "replay.position"
        static let result = "replay.result"
    }
}
