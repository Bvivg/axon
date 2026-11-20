import Foundation

enum SessionStatusText {
    static func headline(_ session: Session, me: String) -> String {
        let seats = session.kind.seatNames
        let seat = session.seat(of: me)
        switch session.status {
        case .waiting:
            return "Waiting for an opponent"
        case .playing:
            let turn = session.hasTurnUserID ? session.playerIds.firstIndex(of: session.turnUserID) : nil
            if seat != nil {
                return session.turnUserID == me ? "Your turn" : "Opponent's turn"
            }
            return turn.map { "\(seats[$0]) to move" } ?? "In progress"
        case .won:
            let winner = session.hasWinnerUserID ? session.playerIds.firstIndex(of: session.winnerUserID) : nil
            if seat != nil {
                return session.winnerUserID == me ? "You won" : "You lost"
            }
            return winner.map { "\(seats[$0]) won" } ?? "Finished"
        case .draw:
            return "Draw"
        default:
            return "Loading"
        }
    }

    static func detail(_ session: Session) -> String? {
        guard session.isFinished else { return nil }
        if session.hasTimedOut, session.timedOut { return "on time" }
        if session.hasResigned, session.resigned { return "by resignation" }
        if session.hasAgreedDraw, session.agreedDraw { return "by agreement" }
        if session.kind == .chess, session.hasChess {
            switch session.chess.end {
            case .checkmate: return "by checkmate"
            case .stalemate: return "by stalemate"
            case .insufficientMaterial: return "insufficient material"
            case .threefoldRepetition: return "threefold repetition"
            case .fiftyMoves: return "fifty-move rule"
            default: return nil
            }
        }
        return nil
    }
}
