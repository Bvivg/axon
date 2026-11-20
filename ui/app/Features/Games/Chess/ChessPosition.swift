import Foundation

enum ChessSide: Sendable, Hashable {
    case white
    case black

    var name: String {
        switch self {
        case .white: "white"
        case .black: "black"
        }
    }
}

enum ChessPieceKind: Character, Sendable, Hashable, CaseIterable {
    case pawn = "p"
    case knight = "n"
    case bishop = "b"
    case rook = "r"
    case queen = "q"
    case king = "k"

    var name: String {
        switch self {
        case .pawn: "pawn"
        case .knight: "knight"
        case .bishop: "bishop"
        case .rook: "rook"
        case .queen: "queen"
        case .king: "king"
        }
    }

    static let promotionOrder: [ChessPieceKind] = [.queen, .rook, .bishop, .knight]
}

struct ChessPiece: Sendable, Hashable {
    let kind: ChessPieceKind
    let side: ChessSide

    init(kind: ChessPieceKind, side: ChessSide) {
        self.kind = kind
        self.side = side
    }

    init?(fenCharacter: Character) {
        guard let kind = ChessPieceKind(rawValue: Character(fenCharacter.lowercased())) else { return nil }
        self.kind = kind
        self.side = fenCharacter.isUppercase ? .white : .black
    }

    var label: String { "\(side.name) \(kind.name)" }
}

struct ChessSquare: Sendable, Hashable, Comparable {
    let file: Int
    let rank: Int

    private static let files = Array("abcdefgh")

    init?(file: Int, rank: Int) {
        guard (0..<8).contains(file), (0..<8).contains(rank) else { return nil }
        self.file = file
        self.rank = rank
    }

    init?<S: StringProtocol>(_ name: S) {
        let bytes = Array(name.utf8)
        guard bytes.count == 2 else { return nil }
        self.init(file: Int(bytes[0]) - 97, rank: Int(bytes[1]) - 49)
    }

    var index: Int { rank * 8 + file }
    var isDark: Bool { (file + rank) % 2 == 0 }
    var fileName: String { String(Self.files[file]) }
    var rankName: String { String(rank + 1) }
    var name: String { fileName + rankName }

    static func < (lhs: ChessSquare, rhs: ChessSquare) -> Bool {
        lhs.index < rhs.index
    }

    static func at(row: Int, column: Int, perspective: Int) -> ChessSquare? {
        perspective == 1
            ? ChessSquare(file: 7 - column, rank: row)
            : ChessSquare(file: column, rank: 7 - row)
    }
}

struct ChessCastling: OptionSet, Sendable, Hashable {
    let rawValue: Int

    static let whiteKingside = ChessCastling(rawValue: 1 << 0)
    static let whiteQueenside = ChessCastling(rawValue: 1 << 1)
    static let blackKingside = ChessCastling(rawValue: 1 << 2)
    static let blackQueenside = ChessCastling(rawValue: 1 << 3)

    init(rawValue: Int) {
        self.rawValue = rawValue
    }

    init<S: StringProtocol>(fen field: S) {
        var rights: ChessCastling = []
        for character in field {
            switch character {
            case "K": rights.insert(.whiteKingside)
            case "Q": rights.insert(.whiteQueenside)
            case "k": rights.insert(.blackKingside)
            case "q": rights.insert(.blackQueenside)
            default: break
            }
        }
        self = rights
    }
}

struct ChessPosition: Sendable, Equatable {
    private(set) var squares: [ChessPiece?]
    let sideToMove: ChessSide
    let castling: ChessCastling
    let enPassant: ChessSquare?
    let halfmoveClock: Int
    let fullmoveNumber: Int

    init(fen: String) {
        let fields = fen.split(separator: " ", omittingEmptySubsequences: true)
        var squares = [ChessPiece?](repeating: nil, count: 64)
        if let placement = fields.first {
            for (offset, row) in placement.split(separator: "/", omittingEmptySubsequences: false).prefix(8).enumerated() {
                let rank = 7 - offset
                var file = 0
                for character in row {
                    if let skip = character.wholeNumberValue {
                        file += skip
                        continue
                    }
                    if let piece = ChessPiece(fenCharacter: character), let square = ChessSquare(file: file, rank: rank) {
                        squares[square.index] = piece
                    }
                    file += 1
                }
            }
        }
        self.squares = squares
        sideToMove = fields.count > 1 && fields[1] == "b" ? .black : .white
        castling = fields.count > 2 ? ChessCastling(fen: fields[2]) : []
        enPassant = fields.count > 3 ? ChessSquare(fields[3]) : nil
        halfmoveClock = fields.count > 4 ? Int(fields[4]) ?? 0 : 0
        fullmoveNumber = fields.count > 5 ? Int(fields[5]) ?? 1 : 1
    }

    subscript(square: ChessSquare) -> ChessPiece? {
        squares[square.index]
    }

    func king(of side: ChessSide) -> ChessSquare? {
        guard let index = squares.firstIndex(of: ChessPiece(kind: .king, side: side)) else { return nil }
        return ChessSquare(file: index % 8, rank: index / 8)
    }
}

struct ChessUCIMove: Sendable, Hashable {
    let from: ChessSquare
    let to: ChessSquare
    let promotion: ChessPieceKind?
    let notation: String

    init?(_ notation: String) {
        guard notation.count == 4 || notation.count == 5,
              let from = ChessSquare(notation.prefix(2)),
              let to = ChessSquare(notation.dropFirst(2).prefix(2))
        else { return nil }
        if notation.count == 5 {
            guard let last = notation.last, let kind = ChessPieceKind(rawValue: Character(last.lowercased())) else { return nil }
            promotion = kind
        } else {
            promotion = nil
        }
        self.from = from
        self.to = to
        self.notation = notation
    }

    static func squares(of notation: String) -> [ChessSquare] {
        guard notation.count >= 4,
              let from = ChessSquare(notation.prefix(2)),
              let to = ChessSquare(notation.dropFirst(2).prefix(2))
        else { return [] }
        return [from, to]
    }
}

struct ChessLegalMoves: Sendable, Equatable {
    private let byOrigin: [ChessSquare: [ChessSquare: [ChessUCIMove]]]

    init(_ notations: [String]) {
        var byOrigin: [ChessSquare: [ChessSquare: [ChessUCIMove]]] = [:]
        for move in notations.compactMap(ChessUCIMove.init) {
            byOrigin[move.from, default: [:]][move.to, default: []].append(move)
        }
        self.byOrigin = byOrigin
    }

    var origins: Set<ChessSquare> { Set(byOrigin.keys) }

    func hasMoves(from square: ChessSquare) -> Bool {
        byOrigin[square] != nil
    }

    func targets(from square: ChessSquare) -> Set<ChessSquare> {
        byOrigin[square].map { Set($0.keys) } ?? []
    }

    func candidates(from: ChessSquare, to: ChessSquare) -> [ChessUCIMove] {
        byOrigin[from]?[to] ?? []
    }

    func promotionChoices(from: ChessSquare, to: ChessSquare) -> [ChessPieceKind] {
        let kinds = Set(candidates(from: from, to: to).compactMap(\.promotion))
        return ChessPieceKind.promotionOrder.filter(kinds.contains)
    }
}

struct ChessPromotionRequest: Sendable, Hashable {
    let from: ChessSquare
    let to: ChessSquare
    let choices: [ChessPieceKind]

    func move(_ kind: ChessPieceKind) -> String {
        from.name + to.name + String(kind.rawValue)
    }
}

enum ChessTapOutcome: Sendable, Equatable {
    case select(ChessSquare?)
    case move(String)
    case promote(ChessPromotionRequest)

    static func resolve(tap square: ChessSquare, selected: ChessSquare?, legal: ChessLegalMoves) -> ChessTapOutcome {
        if let selected, legal.targets(from: selected).contains(square) {
            let candidates = legal.candidates(from: selected, to: square)
            if candidates.count > 1 {
                return .promote(ChessPromotionRequest(from: selected, to: square, choices: legal.promotionChoices(from: selected, to: square)))
            }
            if let only = candidates.first {
                return .move(only.notation)
            }
        }
        if selected == square {
            return .select(nil)
        }
        return .select(legal.hasMoves(from: square) ? square : nil)
    }
}
