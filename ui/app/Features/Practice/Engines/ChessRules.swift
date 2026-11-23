import Foundation

extension ChessPractice {
    static let startFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

    enum Kind {
        static let none = 0
        static let pawn = 1
        static let knight = 2
        static let bishop = 3
        static let rook = 4
        static let queen = 5
        static let king = 6
        static let letters: [Character] = [" ", "p", "n", "b", "r", "q", "k"]
    }

    enum Castling {
        static let whiteKingside = 1
        static let whiteQueenside = 2
        static let blackKingside = 4
        static let blackQueenside = 8
        static let letters: [Character: Int] = ["K": whiteKingside, "Q": whiteQueenside, "k": blackKingside, "q": blackQueenside]
    }

    enum End: String, Sendable {
        case checkmate
        case stalemate
        case insufficientMaterial = "insufficient_material"
        case threefoldRepetition = "threefold_repetition"
        case fiftyMoves = "fifty_moves"
        case moveLimit = "move_limit"
    }

    struct Move: Sendable, Hashable {
        var from: Int
        var to: Int
        var promo: Int

        init(from: Int, to: Int, promo: Int = Kind.none) {
            self.from = from
            self.to = to
            self.promo = promo
        }

        init?(uci: String) {
            let count = uci.utf8.count
            guard count == 4 || count == 5,
                  let from = ChessPractice.square(named: String(uci.prefix(2))),
                  let to = ChessPractice.square(named: String(uci.dropFirst(2).prefix(2)))
            else { return nil }
            var promo = Kind.none
            if count == 5 {
                guard let letter = uci.last, let kind = Kind.letters.firstIndex(of: letter),
                      (Kind.knight...Kind.queen).contains(kind)
                else { return nil }
                promo = kind
            }
            self.init(from: from, to: to, promo: promo)
        }

        var uci: String {
            let base = ChessPractice.squareName(from) + ChessPractice.squareName(to)
            return promo == Kind.none ? base : base + String(Kind.letters[promo])
        }
    }

    struct Undo {
        var captured: Int8
        var capturedSquare: Int
        var castling: Int
        var ep: Int
        var halfmove: Int
        var fullmove: Int
    }

    static func squareName(_ square: Int) -> String {
        String(UnicodeScalar(UInt8(97 + square % 8))) + String(UnicodeScalar(UInt8(49 + square / 8)))
    }

    static func square(named name: String) -> Int? {
        let bytes = Array(name.utf8)
        guard bytes.count == 2, (97...104).contains(bytes[0]), (49...56).contains(bytes[1]) else { return nil }
        return Int(bytes[1] - 49) * 8 + Int(bytes[0] - 97)
    }

    static func piece(_ kind: Int, _ color: Int) -> Int8 {
        Int8(kind | color << 3)
    }

    struct Board: Sendable, Equatable {
        var squares: [Int8]
        var turn: Int
        var castling: Int
        var ep: Int
        var halfmove: Int
        var fullmove: Int
        var whiteKing: Int
        var blackKing: Int

        init?(fen: String) {
            let fields = fen.split(separator: " ").map(String.init)
            guard fields.count == 6,
                  let squares = Self.placement(fields[0]),
                  let whiteKing = squares.firstIndex(of: ChessPractice.piece(Kind.king, 0)),
                  let blackKing = squares.firstIndex(of: ChessPractice.piece(Kind.king, 1)),
                  let turn = ["w": 0, "b": 1][fields[1]],
                  let castling = Self.castlingRights(fields[2]),
                  let ep = fields[3] == "-" ? -1 : ChessPractice.square(named: fields[3]),
                  let halfmove = Int(fields[4]), halfmove >= 0,
                  let fullmove = Int(fields[5]), fullmove >= 1
            else { return nil }
            self.squares = squares
            self.turn = turn
            self.castling = castling
            self.ep = ep
            self.halfmove = halfmove
            self.fullmove = fullmove
            self.whiteKing = whiteKing
            self.blackKing = blackKing
        }

        private static func placement(_ field: String) -> [Int8]? {
            let rows = field.split(separator: "/", omittingEmptySubsequences: false)
            guard rows.count == 8 else { return nil }
            var squares = [Int8](repeating: 0, count: 64)
            for (index, row) in rows.enumerated() {
                guard fill(row, rank: 7 - index, into: &squares) else { return nil }
            }
            let kings = [0, 1].map { color in squares.filter { $0 == ChessPractice.piece(Kind.king, color) }.count }
            return kings == [1, 1] ? squares : nil
        }

        private static func fill(_ row: Substring, rank: Int, into squares: inout [Int8]) -> Bool {
            var file = 0
            for char in row {
                if char.isASCII, let digit = char.wholeNumberValue, (1...8).contains(digit) {
                    file += digit
                    continue
                }
                guard file <= 7, let kind = Kind.letters.firstIndex(of: Character(char.lowercased())), kind > 0 else { return false }
                squares[rank * 8 + file] = ChessPractice.piece(kind, char.isUppercase ? 0 : 1)
                file += 1
            }
            return file == 8
        }

        private static func castlingRights(_ field: String) -> Int? {
            if field == "-" { return 0 }
            var rights = 0
            for char in field {
                guard let flag = Castling.letters[char] else { return nil }
                rights |= flag
            }
            return rights
        }
    }
}

extension ChessPractice.Board {
    private typealias Kind = ChessPractice.Kind

    var placement: String {
        var out = ""
        for rank in stride(from: 7, through: 0, by: -1) {
            var empty = 0
            for file in 0..<8 {
                let piece = Int(squares[rank * 8 + file])
                if piece == 0 {
                    empty += 1
                    continue
                }
                if empty > 0 {
                    out += String(empty)
                    empty = 0
                }
                let letter = Kind.letters[piece & 7]
                out.append(piece >> 3 == 0 ? Character(letter.uppercased()) : letter)
            }
            if empty > 0 { out += String(empty) }
            if rank > 0 { out += "/" }
        }
        return out
    }

    var castlingField: String {
        let flags: [(Int, String)] = [
            (ChessPractice.Castling.whiteKingside, "K"),
            (ChessPractice.Castling.whiteQueenside, "Q"),
            (ChessPractice.Castling.blackKingside, "k"),
            (ChessPractice.Castling.blackQueenside, "q"),
        ]
        let out = flags.filter { castling & $0.0 != 0 }.map(\.1).joined()
        return out.isEmpty ? "-" : out
    }

    var turnField: String { turn == 0 ? "w" : "b" }

    var fen: String {
        let passant = ep >= 0 ? ChessPractice.squareName(ep) : "-"
        return "\(placement) \(turnField) \(castlingField) \(passant) \(halfmove) \(fullmove)"
    }

    var insufficientMaterial: Bool {
        var bishopColors = Set<Int>()
        var minors = 0
        var bishops = 0
        for (square, piece) in squares.enumerated() {
            switch Int(piece) & 7 {
            case Kind.none, Kind.king:
                continue
            case Kind.bishop:
                minors += 1
                bishops += 1
                bishopColors.insert((square % 8 + square / 8) % 2)
            case Kind.knight:
                minors += 1
            default:
                return false
            }
        }
        if minors <= 1 { return true }
        return bishopColors.count == 1 && minors == bishops
    }

    var repetitionKey: String {
        var passant = "-"
        if ep >= 0, legal.contains(where: { $0.to == ep && Int(squares[$0.from]) & 7 == Kind.pawn }) {
            passant = ChessPractice.squareName(ep)
        }
        return "\(placement) \(turnField) \(castlingField) \(passant)"
    }

    func end(positions: [String]) -> ChessPractice.End? {
        var board = self
        if !board.hasLegalMove() { return inCheck ? .checkmate : .stalemate }
        if insufficientMaterial { return .insufficientMaterial }
        if let last = positions.last, positions.filter({ $0 == last }).count >= 3 { return .threefoldRepetition }
        if halfmove >= 100 { return .fiftyMoves }
        return nil
    }

    func san(_ move: ChessPractice.Move) -> String {
        var next = played(move)
        let suffix = next.inCheck ? (next.hasLegalMove() ? "+" : "#") : ""
        return body(move) + suffix
    }

    private func body(_ move: ChessPractice.Move) -> String {
        let kind = Int(squares[move.from]) & 7
        let shift = move.to % 8 - move.from % 8
        if kind == Kind.king, abs(shift) == 2 { return shift > 0 ? "O-O" : "O-O-O" }
        let capture = isCapture(move) ? "x" : ""
        let target = ChessPractice.squareName(move.to)
        if kind == Kind.pawn {
            let file = capture.isEmpty ? "" : String(ChessPractice.squareName(move.from).prefix(1))
            let promotion = move.promo == Kind.none ? "" : "=" + Kind.letters[move.promo].uppercased()
            return file + capture + target + promotion
        }
        return Kind.letters[kind].uppercased() + disambiguation(move) + capture + target
    }

    private func disambiguation(_ move: ChessPractice.Move) -> String {
        var sameFile = false, sameRank = false, ambiguous = false
        for other in legal where other.to == move.to && other.from != move.from && squares[other.from] == squares[move.from] {
            ambiguous = true
            sameFile = sameFile || other.from % 8 == move.from % 8
            sameRank = sameRank || other.from / 8 == move.from / 8
        }
        guard ambiguous else { return "" }
        let name = ChessPractice.squareName(move.from)
        if !sameFile { return String(name.prefix(1)) }
        if !sameRank { return String(name.suffix(1)) }
        return name
    }
}
