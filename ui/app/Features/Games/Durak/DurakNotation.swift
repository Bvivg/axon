import Foundation

struct DurakCard: Hashable, Sendable {
    let code: String
    let rank: Character
    let suit: Character

    init?(_ code: String) {
        guard code.count == 2, let rank = code.first, let suit = code.last,
              DurakCard.ranks.contains(rank), DurakCard.suits.contains(suit)
        else { return nil }
        self.code = code
        self.rank = rank
        self.suit = suit
    }

    static let ranks: [Character] = Array("6789TJQKA")
    static let suits: [Character] = Array("SCDH")

    var rankLabel: String { rank == "T" ? "10" : String(rank) }
    var symbol: String { DurakCard.symbols[suit] ?? "?" }
    var isRed: Bool { suit == "D" || suit == "H" }
    var suitName: String { DurakCard.suitNames[suit] ?? "" }
    var name: String { "\(DurakCard.rankNames[rank] ?? rankLabel) of \(suitName)" }
    var short: String { "\(rankLabel)\(symbol)" }

    private static let symbols: [Character: String] = ["S": "♠", "C": "♣", "D": "♦", "H": "♥"]
    private static let suitNames: [Character: String] = ["S": "spades", "C": "clubs", "D": "diamonds", "H": "hearts"]
    private static let rankNames: [Character: String] = ["T": "10", "J": "jack", "Q": "queen", "K": "king", "A": "ace"]
}

enum DurakNotation {
    static func label(_ move: String) -> String {
        let parts = move.split(separator: " ").map(String.init)
        switch parts.first {
        case "attack" where parts.count > 1:
            return DurakCard(parts[1])?.short ?? move
        case "defend" where parts.count > 2:
            return "\(DurakCard(parts[1])?.short ?? parts[1]) / \(DurakCard(parts[2])?.short ?? parts[2])"
        case "take":
            return "Take"
        case "pass":
            return "Pass"
        default:
            return move
        }
    }

    static func move(for card: String, legal: [String]) -> String? {
        legal.first { $0 == "attack \(card)" || ($0.hasPrefix("defend ") && $0.hasSuffix(" \(card)")) }
    }

    static func openAttack(_ board: DurakBoardState) -> String? {
        board.table.first { !$0.hasDefense }?.attack
    }

    static func task(_ board: DurakBoardState, seat: Int) -> String {
        guard Int(board.attackerSeat) == seat else {
            return openAttack(board).flatMap(DurakCard.init).map { "defend the \($0.name)" } ?? "defend"
        }
        if board.table.isEmpty { return "attack" }
        return board.taking ? "throw in or pass, they take" : "throw in or pass"
    }

    static func ending(_ end: DurakEnd) -> String? {
        switch end {
        case .durak: "last one holding cards"
        case .draw: "both out together"
        default: nil
        }
    }
}
