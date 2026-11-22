import Foundation

extension ChessPractice {
    static let mailbox: [Int] = (0..<120).map { index in
        let row = index / 10, col = index % 10
        guard (2...9).contains(row), (1...8).contains(col) else { return -1 }
        return (row - 2) * 8 + col - 1
    }

    static let mailbox64: [Int] = (0..<64).map { 21 + ($0 / 8) * 10 + $0 % 8 }
    static let knightOffsets = [21, 19, 12, 8, -21, -19, -12, -8]
    static let kingOffsets = [1, -1, 10, -10, 11, 9, -11, -9]
    static let rookOffsets = [1, -1, 10, -10]
    static let bishopOffsets = [11, 9, -11, -9]
    static let promotions = [Kind.queen, Kind.rook, Kind.bishop, Kind.knight]

    static let cornerMask: [Int] = (0..<64).map { square in
        switch square {
        case 0: ~Castling.whiteQueenside
        case 7: ~Castling.whiteKingside
        case 56: ~Castling.blackQueenside
        case 63: ~Castling.blackKingside
        default: ~0
        }
    }
}

extension ChessPractice.Board {
    private typealias Kind = ChessPractice.Kind
    private typealias Move = ChessPractice.Move

    func king(_ color: Int) -> Int {
        color == 0 ? whiteKing : blackKing
    }

    var inCheck: Bool {
        attacked(king(turn), by: turn ^ 1)
    }

    func attacked(_ square: Int, by color: Int) -> Bool {
        let origin = ChessPractice.mailbox64[square]
        let behind = origin + (color == 0 ? -10 : 10)
        let pawn = ChessPractice.piece(Kind.pawn, color)
        let queen = ChessPractice.piece(Kind.queen, color)
        return holds(behind - 1, pawn) || holds(behind + 1, pawn)
            || leaps(origin, ChessPractice.knightOffsets, ChessPractice.piece(Kind.knight, color))
            || leaps(origin, ChessPractice.kingOffsets, ChessPractice.piece(Kind.king, color))
            || slides(origin, ChessPractice.rookOffsets, ChessPractice.piece(Kind.rook, color), queen)
            || slides(origin, ChessPractice.bishopOffsets, ChessPractice.piece(Kind.bishop, color), queen)
    }

    private func holds(_ index: Int, _ piece: Int8) -> Bool {
        let square = ChessPractice.mailbox[index]
        return square >= 0 && squares[square] == piece
    }

    private func leaps(_ origin: Int, _ offsets: [Int], _ piece: Int8) -> Bool {
        for offset in offsets where holds(origin + offset, piece) { return true }
        return false
    }

    private func slides(_ origin: Int, _ offsets: [Int], _ piece: Int8, _ queen: Int8) -> Bool {
        let mailbox = ChessPractice.mailbox
        for offset in offsets {
            var at = origin + offset
            while mailbox[at] >= 0 {
                let found = squares[mailbox[at]]
                if found != 0 {
                    if found == piece || found == queen { return true }
                    break
                }
                at += offset
            }
        }
        return false
    }

    func pseudoMoves() -> [ChessPractice.Move] {
        var moves: [Move] = []
        moves.reserveCapacity(64)
        for from in 0..<64 {
            let piece = Int(squares[from])
            guard piece != 0, piece >> 3 == turn else { continue }
            let origin = ChessPractice.mailbox64[from]
            switch piece & 7 {
            case Kind.pawn:
                pawnMoves(from, origin: origin, into: &moves)
            case Kind.knight:
                leaperMoves(from, origin: origin, offsets: ChessPractice.knightOffsets, into: &moves)
            case Kind.king:
                leaperMoves(from, origin: origin, offsets: ChessPractice.kingOffsets, into: &moves)
                castlingMoves(from, into: &moves)
            case Kind.bishop:
                sliderMoves(from, origin: origin, offsets: ChessPractice.bishopOffsets, into: &moves)
            case Kind.rook:
                sliderMoves(from, origin: origin, offsets: ChessPractice.rookOffsets, into: &moves)
            default:
                sliderMoves(from, origin: origin, offsets: ChessPractice.kingOffsets, into: &moves)
            }
        }
        return moves
    }

    private func leaperMoves(_ from: Int, origin: Int, offsets: [Int], into moves: inout [Move]) {
        let mailbox = ChessPractice.mailbox
        for offset in offsets {
            let to = mailbox[origin + offset]
            guard to >= 0 else { continue }
            let target = Int(squares[to])
            if target == 0 || target >> 3 != turn { moves.append(Move(from: from, to: to)) }
        }
    }

    private func sliderMoves(_ from: Int, origin: Int, offsets: [Int], into moves: inout [Move]) {
        let mailbox = ChessPractice.mailbox
        for offset in offsets {
            var at = origin + offset
            while mailbox[at] >= 0 {
                let to = mailbox[at]
                let target = Int(squares[to])
                if target == 0 {
                    moves.append(Move(from: from, to: to))
                    at += offset
                    continue
                }
                if target >> 3 != turn { moves.append(Move(from: from, to: to)) }
                break
            }
        }
    }

    private func addPawn(_ from: Int, _ to: Int, into moves: inout [Move]) {
        let rank = to / 8
        if rank == 0 || rank == 7 {
            for kind in ChessPractice.promotions { moves.append(Move(from: from, to: to, promo: kind)) }
        } else {
            moves.append(Move(from: from, to: to))
        }
    }

    private func pawnMoves(_ from: Int, origin: Int, into moves: inout [Move]) {
        let mailbox = ChessPractice.mailbox
        let forward = turn == 0 ? 10 : -10
        let one = mailbox[origin + forward]
        if one >= 0, squares[one] == 0 {
            addPawn(from, one, into: &moves)
            let two = mailbox[origin + 2 * forward]
            if from / 8 == (turn == 0 ? 1 : 6), two >= 0, squares[two] == 0 { moves.append(Move(from: from, to: two)) }
        }
        for side in stride(from: -1, through: 1, by: 2) {
            let to = mailbox[origin + forward + side]
            guard to >= 0 else { continue }
            let target = Int(squares[to])
            if (target != 0 && target >> 3 != turn) || to == ep { addPawn(from, to, into: &moves) }
        }
    }

    private func castlingMoves(_ from: Int, into moves: inout [Move]) {
        let us = turn, them = turn ^ 1
        let rank = us == 0 ? 0 : 7
        let kingside = us == 0 ? ChessPractice.Castling.whiteKingside : ChessPractice.Castling.blackKingside
        let queenside = us == 0 ? ChessPractice.Castling.whiteQueenside : ChessPractice.Castling.blackQueenside
        guard from == rank * 8 + 4, castling & (kingside | queenside) != 0, !attacked(from, by: them) else { return }
        let rook = ChessPractice.piece(Kind.rook, us)
        func empty(_ files: [Int]) -> Bool { files.allSatisfy { squares[rank * 8 + $0] == 0 } }
        func safe(_ files: [Int]) -> Bool { files.allSatisfy { !attacked(rank * 8 + $0, by: them) } }
        if castling & kingside != 0, squares[rank * 8 + 7] == rook, empty([5, 6]), safe([5, 6]) {
            moves.append(Move(from: from, to: rank * 8 + 6))
        }
        if castling & queenside != 0, squares[rank * 8] == rook, empty([1, 2, 3]), safe([3, 2]) {
            moves.append(Move(from: from, to: rank * 8 + 2))
        }
    }

    mutating func make(_ move: ChessPractice.Move) -> ChessPractice.Undo {
        let piece = squares[move.from]
        let kind = Int(piece) & 7
        let us = turn
        var undo = ChessPractice.Undo(
            captured: squares[move.to], capturedSquare: move.to, castling: castling, ep: ep, halfmove: halfmove, fullmove: fullmove
        )
        var placed = piece
        squares[move.from] = 0
        ep = -1
        if kind == Kind.pawn {
            if move.to == undo.ep {
                undo.capturedSquare = move.to + (us == 0 ? -8 : 8)
                undo.captured = squares[undo.capturedSquare]
                squares[undo.capturedSquare] = 0
            }
            if abs(move.to - move.from) == 16 { ep = (move.from + move.to) / 2 }
            if move.promo != Kind.none { placed = ChessPractice.piece(move.promo, us) }
        } else if kind == Kind.king {
            castle(move, back: false)
            castling &= us == 0 ? ~(ChessPractice.Castling.whiteKingside | ChessPractice.Castling.whiteQueenside)
                : ~(ChessPractice.Castling.blackKingside | ChessPractice.Castling.blackQueenside)
            if us == 0 { whiteKing = move.to } else { blackKing = move.to }
        }
        squares[move.to] = placed
        castling &= ChessPractice.cornerMask[move.from] & ChessPractice.cornerMask[move.to]
        halfmove = kind == Kind.pawn || undo.captured != 0 ? 0 : halfmove + 1
        if us == 1 { fullmove += 1 }
        turn = us ^ 1
        return undo
    }

    mutating func unmake(_ move: ChessPractice.Move, _ undo: ChessPractice.Undo) {
        turn ^= 1
        let moved = move.promo != Kind.none ? ChessPractice.piece(Kind.pawn, turn) : squares[move.to]
        squares[move.from] = moved
        squares[move.to] = 0
        if undo.captured != 0 { squares[undo.capturedSquare] = undo.captured }
        if Int(moved) & 7 == Kind.king {
            castle(move, back: true)
            if turn == 0 { whiteKing = move.from } else { blackKing = move.from }
        }
        castling = undo.castling
        ep = undo.ep
        halfmove = undo.halfmove
        fullmove = undo.fullmove
    }

    private mutating func castle(_ move: Move, back: Bool) {
        let rank = move.from / 8 * 8
        let rook: (from: Int, to: Int)
        switch move.to - move.from {
        case 2: rook = (rank + 7, rank + 5)
        case -2: rook = (rank, rank + 3)
        default: return
        }
        let source = back ? rook.to : rook.from, target = back ? rook.from : rook.to
        squares[target] = squares[source]
        squares[source] = 0
    }

    func played(_ move: ChessPractice.Move) -> ChessPractice.Board {
        var next = self
        _ = next.make(move)
        return next
    }

    mutating func legalMoves() -> [ChessPractice.Move] {
        let us = turn
        var out: [Move] = []
        for move in pseudoMoves() {
            let undo = make(move)
            if !attacked(king(us), by: turn) { out.append(move) }
            unmake(move, undo)
        }
        return out
    }

    mutating func hasLegalMove() -> Bool {
        let us = turn
        for move in pseudoMoves() {
            let undo = make(move)
            let legal = !attacked(king(us), by: turn)
            unmake(move, undo)
            if legal { return true }
        }
        return false
    }

    var legal: [ChessPractice.Move] {
        var board = self
        return board.legalMoves()
    }

    func perft(_ depth: Int) -> Int {
        var board = self
        return board.countLeaves(depth)
    }

    private mutating func countLeaves(_ depth: Int) -> Int {
        let moves = legalMoves()
        if depth <= 1 { return depth == 1 ? moves.count : 1 }
        var total = 0
        for move in moves {
            let undo = make(move)
            total += countLeaves(depth - 1)
            unmake(move, undo)
        }
        return total
    }

    func isCapture(_ move: ChessPractice.Move) -> Bool {
        captured(move) != Kind.none
    }

    func captured(_ move: ChessPractice.Move) -> Int {
        if squares[move.to] != 0 { return Int(squares[move.to]) & 7 }
        return Int(squares[move.from]) & 7 == Kind.pawn && move.to == ep ? Kind.pawn : Kind.none
    }
}
