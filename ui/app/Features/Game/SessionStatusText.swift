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
        if session.hasMoveLimit, session.moveLimit { return "move limit reached" }
        switch session.kind {
        case .chess: return session.hasChess ? chessEndings[session.chess.end] : nil
        case .checkers: return session.hasCheckers ? checkersEnding(session) : nil
        case .durak: return session.hasDurak ? DurakNotation.ending(session.durak.end) : nil
        case .ticTacToe, .quoridor: return nil
        }
    }

    static func hint(_ session: Session, me: String) -> String? {
        if session.kind == .durak {
            guard session.status == .playing, session.turnUserID == me, let seat = session.seat(of: me) else { return nil }
            return "Your move · \(DurakNotation.task(session.durak, seat: seat))"
        }
        guard session.kind == .checkers, session.status == .playing, session.turnUserID == me,
              session.checkers.legalMoves.contains(where: { CheckersMove(notation: $0)?.jump == true })
        else { return nil }
        return "You must capture"
    }

    private static func checkersEnding(_ session: Session) -> String? {
        switch session.checkers.end {
        case .noMoves:
            guard session.status == .won,
                  let winner = session.playerIds.firstIndex(of: session.winnerUserID),
                  let loser = CheckersSide(rawValue: 1 - winner)
            else { return "no moves left" }
            return CheckersPosition(session.checkers.squares).count(loser) == 0 ? "by capturing every piece" : "by blockade"
        case .fortyMoves: return "forty-move rule"
        case .moveLimit: return "move limit"
        default: return nil
        }
    }

    private static let chessEndings: [ChessEnd: String] = [
        .checkmate: "by checkmate",
        .stalemate: "by stalemate",
        .insufficientMaterial: "insufficient material",
        .threefoldRepetition: "threefold repetition",
        .fiftyMoves: "fifty-move rule",
    ]
}
