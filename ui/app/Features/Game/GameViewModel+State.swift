import Foundation

extension GameViewModel {
    var canJoin: Bool {
        guard let session else { return false }
        return session.status == .waiting && !session.playerIds.contains(me)
    }

    var canResign: Bool {
        guard let session, mySeat != nil else { return false }
        return session.status == .playing || session.status == .waiting
    }

    var canOfferDraw: Bool {
        guard let session, mySeat != nil, kind.supportsDrawOffers else { return false }
        return session.status == .playing && !session.hasDrawOfferUserID
    }

    var drawOfferedToMe: Bool {
        guard let session, mySeat != nil, session.status == .playing, session.hasDrawOfferUserID else { return false }
        return session.drawOfferUserID != me
    }

    var drawOfferedByMe: Bool {
        guard let session, session.status == .playing, session.hasDrawOfferUserID else { return false }
        return session.drawOfferUserID == me
    }

    var canUndo: Bool {
        guard let session, mySeat != nil, kind == .ticTacToe else { return false }
        let rated = session.hasRated && session.rated
        return session.status == .playing && session.ply > 0 && !rated && connected && !pending
    }

    var resignPrompt: String {
        session?.status == .waiting ? "Cancel this game?" : "Resign this game?"
    }

    var moves: [String] {
        guard let session else { return [] }
        switch session.kind {
        case .chess: return session.chess.san
        case .quoridor: return session.quoridor.moves
        case .checkers: return session.checkers.moves
        case .durak: return session.durak.moves.map(DurakNotation.label)
        case .ticTacToe: return []
        }
    }

    var spectators: Int {
        guard let session, session.hasSpectators else { return 0 }
        return Int(session.spectators)
    }
}
