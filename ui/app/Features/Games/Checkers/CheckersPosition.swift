import Foundation

enum CheckersSide: Int, Sendable, Hashable, CaseIterable {
    case black = 0
    case white = 1

    var name: String {
        switch self {
        case .black: "black"
        case .white: "white"
        }
    }
}

struct CheckersPiece: Sendable, Hashable {
    let side: CheckersSide
    let isKing: Bool

    init(side: CheckersSide, isKing: Bool) {
        self.side = side
        self.isKing = isKing
    }

    init?(_ code: CheckersPieceCode) {
        switch code {
        case .blackMan: self.init(side: .black, isKing: false)
        case .blackKing: self.init(side: .black, isKing: true)
        case .whiteMan: self.init(side: .white, isKing: false)
        case .whiteKing: self.init(side: .white, isKing: true)
        default: return nil
        }
    }

    var label: String { "\(side.name) \(isKing ? "king" : "man")" }
}

struct CheckersPosition: Sendable, Equatable {
    private(set) var squares: [CheckersPiece?]

    init(_ codes: [CheckersPieceCode]) {
        guard codes.count == Checkers.squares else {
            squares = Self.start
            return
        }
        squares = codes.map(CheckersPiece.init)
    }

    subscript(square: CheckersSquare) -> CheckersPiece? {
        squares[square.index]
    }

    func count(_ side: CheckersSide) -> Int {
        squares.filter { $0?.side == side }.count
    }

    static let start: [CheckersPiece?] = (0..<Checkers.squares).map { index in
        if index < 12 { return CheckersPiece(side: .black, isKing: false) }
        if index >= 20 { return CheckersPiece(side: .white, isKing: false) }
        return nil
    }
}
