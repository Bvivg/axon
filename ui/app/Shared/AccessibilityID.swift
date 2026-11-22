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

        static func provider(_ name: String) -> String { "auth.provider.\(name)" }
    }

    enum Home {
        static let palette = "home.palette"

        static func tab(_ name: String) -> String { "home.tab.\(name)" }
    }

    enum Lobby {
        static let root = "lobby"
        static let start = "lobby.start"
        static let joinID = "lobby.joinID"
        static let join = "lobby.join"
        static let cancelMatch = "lobby.cancelMatch"
        static let error = "lobby.error"

        static func game(_ game: String) -> String { "lobby.game.\(game)" }
        static func create(_ game: String) -> String { "lobby.create.\(game)" }
        static func quickMatch(_ game: String) -> String { "lobby.quickMatch.\(game)" }
        static func practice(_ game: String) -> String { "lobby.practice.\(game)" }
        static func session(_ id: String) -> String { "lobby.session.\(id)" }
    }

    enum Chats {
        static let root = "chats"
        static let compose = "chats.compose"
        static let conversation = "chats.conversation"
        static let feed = "chats.feed"
        static let composer = "chats.composer"
        static let send = "chats.send"
        static let attach = "chats.attach"
        static let attachment = "chats.attachment"
        static let record = "chats.record"
        static let outgoing = "chats.outgoing"
        static let notice = "chats.notice"
        static let more = "chats.more"
        static let info = "chats.info"
        static let forwardSelection = "chats.selection.forward"
        static let deleteSelection = "chats.selection.delete"
        static let forwardSend = "chats.forward.send"
        static let groupName = "chats.group.name"
        static let createGroup = "chats.group.create"
        static let addMembers = "chats.members.add"
        static let confirmAdd = "chats.members.confirm"
        static let rename = "chats.group.rename"
        static let mediaLibrary = "chats.media"
        static let mediaSection = "chats.media.section"
        static let leave = "chats.leave"

        static func room(_ id: String) -> String { "chats.room.\(id)" }
        static func message(_ id: String) -> String { "chats.message.\(id)" }
        static func unread(_ id: String) -> String { "chats.unread.\(id)" }
        static func person(_ id: String) -> String { "chats.person.\(id)" }
        static func forwardTarget(_ id: String) -> String { "chats.forward.\(id)" }
        static func searchResult(_ id: String) -> String { "chats.search.\(id)" }
        static func removeMember(_ id: String) -> String { "chats.members.remove.\(id)" }
    }

    enum Durak {
        static let take = "durak.take"
        static let pass = "durak.pass"
        static let stock = "durak.stock"
        static let table = "durak.table"
        static let opponent = "durak.opponent"

        static func card(_ code: String) -> String { "durak.card.\(code)" }
    }

    enum Calls {
        static let root = "calls"
        static let filter = "calls.filter"
        static let newCall = "calls.new"
        static let screen = "calls.screen"
        static let status = "calls.status"
        static let banner = "calls.banner"
        static let minimize = "calls.minimize"
        static let accept = "calls.accept"
        static let acceptAudio = "calls.acceptAudio"
        static let decline = "calls.decline"
        static let mute = "calls.mute"
        static let camera = "calls.camera"
        static let add = "calls.add"
        static let end = "calls.end"
        static let close = "calls.close"
        static let callBack = "calls.callBack"
        static let start = "calls.start"
        static let startAudio = "calls.start.audio"
        static let startVideo = "calls.start.video"
        static let detail = "calls.detail"
        static let detailAudio = "calls.detail.audio"
        static let detailVideo = "calls.detail.video"
        static let detailChat = "calls.detail.chat"

        static func call(_ id: String) -> String { "calls.call.\(id)" }
    }

    enum Profile {
        static let root = "profile"
        static let save = "profile.save"
        static let saved = "profile.saved"
        static let email = "profile.email"
        static let username = "profile.username"
        static let firstName = "profile.firstName"
        static let lastName = "profile.lastName"
        static let nickname = "profile.nickname"
        static let photo = "profile.photo"

        static func page(_ name: String) -> String { "profile.page.\(name)" }
        static func endSession(_ id: String) -> String { "profile.endSession.\(id)" }
        static func connect(_ provider: String) -> String { "profile.connect.\(provider)" }
        static func theme(_ name: String) -> String { "profile.theme.\(name)" }
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
        static let moves = "game.moves"

        static func clock(_ seat: Int) -> String { "game.clock.\(seat)" }
        static func ticTacToeCell(_ index: Int) -> String { "ttt.cell.\(index)" }
        static func chessSquare(_ square: String) -> String { "chess.square.\(square)" }
        static func quoridorCell(_ cell: String) -> String { "quoridor.cell.\(cell)" }
        static func quoridorWall(_ wall: String) -> String { "quoridor.wall.\(wall)" }
        static func checkersSquare(_ number: Int) -> String { "checkers.square.\(number)" }
    }

    enum Tournaments {
        static let entry = "tournaments.entry"
        static let root = "tournaments"
        static let create = "tournaments.create"
        static let name = "tournaments.name"
        static let submit = "tournaments.submit"
        static let status = "tournament.status"
        static let register = "tournament.register"
        static let withdraw = "tournament.withdraw"
        static let start = "tournament.start"
        static let cancel = "tournament.cancel"
        static let winner = "tournament.winner"
        static let players = "tournament.players"
        static let error = "tournament.error"

        static func row(_ id: String) -> String { "tournaments.row.\(id)" }
        static func match(_ round: Int, _ board: Int) -> String { "tournament.match.\(round).\(board)" }
    }

    enum Practice {
        static let root = "practice"
        static let status = "practice.status"
        static let opponent = "practice.opponent"
        static let side = "practice.side"
        static let undo = "practice.undo"
        static let restart = "practice.restart"
        static let again = "practice.again"
        static let result = "practice.result"
    }

    enum Analysis {
        static let panel = "analysis.panel"
        static let bar = "analysis.bar"
        static let chart = "analysis.chart"
        static let evaluation = "analysis.evaluation"
        static let verdict = "analysis.verdict"
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
