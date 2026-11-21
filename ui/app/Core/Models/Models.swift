import Foundation

typealias User = Axon_Auth_V1_User
typealias TokenPair = Axon_Auth_V1_TokenPair
typealias PublicProfile = Axon_Auth_V1_PublicProfile
typealias Session = Axon_Game_V1_Session
typealias SessionStatus = Axon_Game_V1_SessionStatus
typealias SessionCommand = Axon_Game_V1_SessionCommand
typealias SessionEvent = Axon_Game_V1_SessionEvent
typealias SessionError = Axon_Game_V1_SessionError
typealias MatchTicket = Axon_Game_V1_MatchTicket
typealias MatchStatus = Axon_Game_V1_MatchStatus
typealias HistoryFrame = Axon_Game_V1_HistoryFrame
typealias SessionHistory = Axon_Game_V1_GetSessionHistoryResponse
typealias Mark = Axon_Game_V1_Mark
typealias ChessEnd = Axon_Game_V1_ChessEnd
typealias DrawAction = Axon_Game_V1_DrawAction
typealias TicTacToeBoardState = Axon_Game_V1_TicTacToeBoard
typealias ChessBoardState = Axon_Game_V1_ChessBoard
typealias QuoridorBoardState = Axon_Game_V1_QuoridorBoard

enum GameKind: String, CaseIterable, Identifiable, Hashable, Sendable {
    case ticTacToe = "tictactoe"
    case chess
    case quoridor

    var id: String { rawValue }

    init(game: String) {
        self = GameKind(rawValue: game) ?? .ticTacToe
    }

    var title: String {
        switch self {
        case .ticTacToe: "Tic-Tac-Toe"
        case .chess: "Chess"
        case .quoridor: "Quoridor"
        }
    }

    var symbol: String {
        switch self {
        case .ticTacToe: "number"
        case .chess: "crown"
        case .quoridor: "square.grid.3x3.middle.filled"
        }
    }

    var seatNames: [String] {
        switch self {
        case .ticTacToe: ["X", "O"]
        case .chess: ["White", "Black"]
        case .quoridor: ["Violet", "Green"]
        }
    }

    var defaultClock: Duration {
        switch self {
        case .ticTacToe: .seconds(300)
        case .chess, .quoridor: .seconds(600)
        }
    }

    var supportsDrawOffers: Bool { self == .chess }
}

extension Session {
    var kind: GameKind { GameKind(game: game) }

    var revisionValue: UInt32 { hasRevision ? revision : ply }

    var isFinished: Bool { status == .won || status == .draw }

    func seat(of userID: String) -> Int? {
        playerIds.firstIndex(of: userID)
    }

    func isSpectator(_ userID: String) -> Bool {
        playerIds.count == 2 && !playerIds.contains(userID)
    }

    func remainingMilliseconds(seat: Int) -> UInt32? {
        guard hasClock else { return nil }
        switch seat {
        case 0: return clock.hasXRemainingMs ? clock.xRemainingMs : nil
        case 1: return clock.hasORemainingMs ? clock.oRemainingMs : nil
        default: return nil
        }
    }
}
