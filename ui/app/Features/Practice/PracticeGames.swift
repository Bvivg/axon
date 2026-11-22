import Foundation

extension TicTacToePractice: PracticeGame {
    static let kind = GameKind.ticTacToe

    static func session(_ position: Position, legal: [String], viewer: Int?) -> Session {
        Session.with {
            $0.game = kind.rawValue
            $0.ticTacToe = TicTacToeBoardState.with { board in
                board.cells = position.cells.map { $0 == 1 ? .x : $0 == 2 ? .o : .unspecified }
                board.winningCells = position.winningLine.map(UInt32.init)
            }
        }
    }

    static func moves(_ position: Position) -> [String] { [] }

    static func ending(_ position: Position) -> String? {
        position.winner == nil && position.isFull ? "the board is full" : nil
    }
}

extension ChessPractice: PracticeGame {
    static let kind = GameKind.chess

    static func session(_ position: Position, legal: [String], viewer: Int?) -> Session {
        Session.with {
            $0.game = kind.rawValue
            $0.chess = ChessBoardState.with { board in
                board.fen = position.fen
                board.san = position.san
                if let last = position.lastMove { board.lastMove = last }
                board.check = position.inCheck
                board.legalMoves = legal
                board.end = endings[position.end] ?? .unspecified
            }
        }
    }

    static func moves(_ position: Position) -> [String] { position.san }

    static func ending(_ position: Position) -> String? {
        switch position.end {
        case .checkmate: "by checkmate"
        case .stalemate: "by stalemate"
        case .insufficientMaterial: "insufficient material"
        case .threefoldRepetition: "threefold repetition"
        case .fiftyMoves: "fifty-move rule"
        case .moveLimit: "move limit reached"
        case nil: nil
        }
    }

    static func hint(_ position: Position, seat: Int) -> String? {
        position.inCheck && position.end == nil ? "Check" : nil
    }

    private static let endings: [End?: ChessEnd] = [
        .checkmate: .checkmate,
        .stalemate: .stalemate,
        .insufficientMaterial: .insufficientMaterial,
        .threefoldRepetition: .threefoldRepetition,
        .fiftyMoves: .fiftyMoves,
    ]

    static func san(of uci: String, in fen: String) -> String? {
        guard var board = Board(fen: fen), let move = Move(uci: uci), board.legalMoves().contains(move) else { return nil }
        return board.san(move)
    }
}

extension CheckersPractice: PracticeGame {
    static let kind = GameKind.checkers

    static func session(_ position: Position, legal: [String], viewer: Int?) -> Session {
        Session.with {
            $0.game = kind.rawValue
            $0.checkers = CheckersBoardState.with { board in
                board.squares = position.squares.map { CheckersPieceCode(rawValue: $0.rawValue) ?? .unspecified }
                board.moves = position.moves
                if let last = position.lastMove { board.lastMove = last }
                board.legalMoves = legal
                board.quietPlies = UInt32(position.quiet)
                board.end = switch position.end {
                case .noMoves: .noMoves
                case .fortyMoves: .fortyMoves
                case .moveLimit: .moveLimit
                case nil: .unspecified
                }
            }
        }
    }

    static func moves(_ position: Position) -> [String] { position.moves }

    static func ending(_ position: Position) -> String? {
        switch position.end {
        case .noMoves:
            guard case .won(let winner) = outcome(position) else { return "no moves left" }
            return position.count(1 - winner) == 0 ? "by capturing every piece" : "by blockade"
        case .fortyMoves: return "forty-move rule"
        case .moveLimit: return "move limit reached"
        case nil: return nil
        }
    }

    static func hint(_ position: Position, seat: Int) -> String? {
        guard outcome(position) == nil,
              legalMoves(position).contains(where: { CheckersMove(notation: $0)?.jump == true })
        else { return nil }
        return "you must capture"
    }
}

extension QuoridorPractice: PracticeGame {
    static let kind = GameKind.quoridor

    static func session(_ position: Position, legal: [String], viewer: Int?) -> Session {
        Session.with {
            $0.game = kind.rawValue
            $0.quoridor = QuoridorBoardState.with { board in
                board.pawns = position.pawnNames
                board.walls = position.walls
                board.wallsLeft = position.wallsLeft.map(UInt32.init)
                board.moves = position.moves
                if let last = position.lastMove { board.lastMove = last }
                board.legalMoves = legal
            }
        }
    }

    static func moves(_ position: Position) -> [String] { position.moves }

    static func ending(_ position: Position) -> String? {
        outcome(position) == .draw ? "move limit reached" : nil
    }
}

extension DurakPractice: PracticeGame {
    static let kind = GameKind.durak
    static let opponents: [PracticeOpponent] = [.computer(.medium)]
    static let choosesSide = false

    static func session(_ position: Position, legal: [String], viewer: Int?) -> Session {
        Session.with {
            $0.game = kind.rawValue
            $0.durak = board(position, legal: legal, viewer: viewer)
        }
    }

    static func board(_ position: Position, legal: [String], viewer: Int?) -> DurakBoardState {
        DurakBoardState.with { board in
            board.hands = position.hands.enumerated().map { seat, cards in
                DurakHand.with { hand in
                    hand.count = UInt32(cards.count)
                    if viewer == nil || viewer == seat {
                        hand.cards = cards
                    } else {
                        hand.hidden = true
                    }
                }
            }
            board.trump = position.trump
            board.stock = UInt32(position.stockCount)
            board.table = position.table.map { pair in
                DurakPair.with {
                    $0.attack = pair.attack
                    if let defense = pair.defense { $0.defense = defense }
                }
            }
            board.discard = UInt32(position.discardCount)
            board.attackerSeat = UInt32(position.attacker)
            board.taking = position.taking
            board.boutLimit = UInt32(position.boutLimit)
            board.moves = position.moves
            if let last = position.lastMove { board.lastMove = last }
            board.legalMoves = legal
            board.end = switch outcome(position) {
            case .won: .durak
            case .draw: .draw
            case nil: .unspecified
            }
        }
    }

    static func moves(_ position: Position) -> [String] { position.moves.map(DurakNotation.label) }

    static func ending(_ position: Position) -> String? {
        switch outcome(position) {
        case .won: "last one holding cards"
        case .draw: position.finished.over ? "both out together" : "move limit reached"
        case nil: nil
        }
    }

    static func hint(_ position: Position, seat: Int) -> String? {
        DurakNotation.task(board(position, legal: [], viewer: nil), seat: seat)
    }
}
