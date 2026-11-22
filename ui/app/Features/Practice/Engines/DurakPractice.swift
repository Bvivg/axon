import Foundation

enum DurakPractice: PracticeRules {
    static let ranks = Array("6789TJQKA")
    static let suits = Array("SCDH")
    static let handSize = 6
    static let deckSize = 36
    static let maxPlies = 1000

    enum End: String, Sendable {
        case durak
        case draw
        case moveLimit = "move_limit"
    }

    struct Pair: Sendable, Equatable {
        var attack: String
        var defense: String?
    }

    struct Position: Sendable, Equatable {
        var hands: [[String]]
        var stock: [String]
        var trump: String
        var table: [Pair]
        var discard: [String]
        var attacker: Int
        var boutLimit: Int
        var taking: Bool
        var moves: [String]

        init(
            hands: [[String]],
            stock: [String],
            trump: String,
            table: [Pair] = [],
            discard: [String] = [],
            attacker: Int,
            boutLimit: Int,
            taking: Bool = false,
            moves: [String] = []
        ) {
            self.hands = hands
            self.stock = stock
            self.trump = trump
            self.table = table
            self.discard = discard
            self.attacker = attacker
            self.boutLimit = boutLimit
            self.taking = taking
            self.moves = moves
        }

        var defender: Int { 1 - attacker }
        var stockCount: Int { stock.count }
        var discardCount: Int { discard.count }
        var lastMove: String? { moves.last }

        var openPair: Int? { table.firstIndex { $0.defense == nil } }

        var openAttack: String? { openPair.map { table[$0].attack } }

        var turn: Int { !taking && openPair != nil ? defender : attacker }

        var finished: (over: Bool, winner: Int?) {
            guard stock.isEmpty, table.isEmpty else { return (false, nil) }
            let out = (hands[0].isEmpty, hands[1].isEmpty)
            switch out {
            case (true, true): return (true, nil)
            case (true, false): return (true, 0)
            case (false, true): return (true, 1)
            case (false, false): return (false, nil)
            }
        }

        var end: End? {
            let result = finished
            if result.over { return result.winner == nil ? .draw : .durak }
            return moves.count >= DurakPractice.maxPlies ? .moveLimit : nil
        }

        var cards: [String] {
            hands[0] + hands[1] + stock + table.flatMap { [$0.attack] + ($0.defense.map { [$0] } ?? []) } + discard
        }
    }

    static func rank(_ card: String) -> Int {
        card.first.flatMap { ranks.firstIndex(of: $0) } ?? -1
    }

    static func suit(_ card: String) -> Character {
        card.count == 2 ? card[card.index(after: card.startIndex)] : "?"
    }

    static func isCard(_ card: String) -> Bool {
        card.count == 2 && rank(card) >= 0 && suits.contains(suit(card))
    }

    static func beats(_ defense: String, _ attack: String, trump: Character) -> Bool {
        if suit(defense) == suit(attack) { return rank(defense) > rank(attack) }
        return suit(defense) == trump
    }

    static func sortHand(_ hand: [String], trump: Character) -> [String] {
        func weight(_ card: String) -> Int {
            let suit = suit(card)
            return (suit == trump ? 4 : suits.firstIndex(of: suit) ?? 0) * 16 + rank(card)
        }
        return hand.sorted { weight($0) < weight($1) }
    }

    static func deck() -> [String] {
        suits.flatMap { suit in ranks.map { String([$0, suit]) } }
    }

    static func shuffled(seed: UInt64) -> [String] {
        var random = PracticeRandom(seed: seed)
        var cards = deck()
        for index in stride(from: cards.count - 1, to: 0, by: -1) {
            let other = Int(random.next() % UInt64(index + 1))
            cards.swapAt(index, other)
        }
        return cards
    }

    static func fromDeck(_ cards: [String]) -> Position {
        let trump = cards[2 * handSize]
        let suit = suit(trump)
        let hands = [sortHand(Array(cards[0..<handSize]), trump: suit), sortHand(Array(cards[handSize..<2 * handSize]), trump: suit)]
        let attacker = firstAttacker(hands, trump: suit)
        let stock = Array(cards[(2 * handSize + 1)...]) + [trump]
        return Position(hands: hands, stock: stock, trump: trump, attacker: attacker, boutLimit: min(handSize, hands[1 - attacker].count))
    }

    static func firstAttacker(_ hands: [[String]], trump: Character) -> Int {
        var seat = 0
        var lowest = ranks.count
        for (at, hand) in hands.enumerated() {
            for card in hand where suit(card) == trump && rank(card) < lowest {
                lowest = rank(card)
                seat = at
            }
        }
        return seat
    }

    private static func onTable(_ position: Position, _ card: String) -> Bool {
        let wanted = rank(card)
        return position.table.contains { rank($0.attack) == wanted || ($0.defense.map { rank($0) == wanted } ?? false) }
    }

    private static func canAttack(_ position: Position, _ card: String) -> Bool {
        guard position.turn == position.attacker, position.table.count < position.boutLimit else { return false }
        return position.table.isEmpty || onTable(position, card)
    }

    static func start(seed: UInt64) -> Position {
        fromDeck(shuffled(seed: seed))
    }

    static func turn(_ position: Position) -> Int {
        position.turn
    }

    static func outcome(_ position: Position) -> PracticeOutcome? {
        let result = position.finished
        if result.over { return result.winner.map { .won(seat: $0) } ?? .draw }
        return position.moves.count >= maxPlies ? .draw : nil
    }

    static func legalMoves(_ position: Position) -> [String] {
        guard outcome(position) == nil else { return [] }
        let seat = position.turn
        let trump = suit(position.trump)
        if seat != position.attacker, let open = position.openAttack {
            return position.hands[seat].filter { beats($0, open, trump: trump) }.map { "defend \(open) \($0)" } + ["take"]
        }
        let attacks = position.hands[seat].filter { canAttack(position, $0) }.map { "attack \($0)" }
        return position.table.isEmpty ? attacks : attacks + ["pass"]
    }

    static func apply(_ move: String, to position: Position) -> Position? {
        guard legalMoves(position).contains(move) else { return nil }
        let seat = position.turn
        let parts = move.split(separator: " ").map(String.init)
        var next = position
        next.moves.append(move)
        switch parts[0] {
        case "attack":
            next.hands[seat].removeAll { $0 == parts[1] }
            next.table.append(Pair(attack: parts[1]))
        case "defend":
            next.hands[seat].removeAll { $0 == parts[2] }
            if let open = next.openPair { next.table[open].defense = parts[2] }
        case "take":
            next.taking = true
        default:
            endBout(&next)
        }
        return next
    }

    private static func endBout(_ position: inout Position) {
        let attacker = position.attacker, defender = position.defender
        let trump = suit(position.trump)
        let played = position.table.flatMap { [$0.attack] + ($0.defense.map { [$0] } ?? []) }
        if position.taking {
            position.hands[defender] += played
        } else {
            position.discard += played
        }
        position.table = []
        for seat in [attacker, defender] {
            let need = max(0, handSize - position.hands[seat].count)
            let drawn = Array(position.stock.prefix(need))
            position.stock.removeFirst(drawn.count)
            position.hands[seat] = sortHand(position.hands[seat] + drawn, trump: trump)
        }
        if !position.taking { position.attacker = defender }
        position.taking = false
        position.boutLimit = min(handSize, position.hands[position.defender].count)
    }

    static func botMove(_ position: Position, level: PracticeLevel, using random: inout some RandomNumberGenerator) -> String? {
        let legal = legalMoves(position)
        guard let first = legal.first else { return nil }
        let trump = suit(position.trump)
        func cost(_ move: String) -> Int {
            let card = String(move.suffix(2))
            return (suit(card) == trump ? 100 : 0) + rank(card)
        }
        func cheapest(_ moves: [String]) -> String {
            moves.dropFirst().reduce(moves[0]) { cost($1) < cost($0) ? $1 : $0 }
        }
        let defences = legal.filter { $0.hasPrefix("defend ") }
        if !defences.isEmpty { return cheapest(defences) }
        if legal.contains("take") { return "take" }
        let attacks = legal.filter { $0.hasPrefix("attack ") }
        if attacks.isEmpty { return first }
        if position.table.isEmpty { return cheapest(attacks) }
        let jack = ranks.firstIndex(of: "J") ?? 5
        let throwable = attacks.filter {
            let card = String($0.suffix(2))
            return suit(card) != trump && rank(card) <= jack
        }
        if !throwable.isEmpty, position.taking || !position.stock.isEmpty { return cheapest(throwable) }
        return legal.contains("pass") ? "pass" : cheapest(attacks)
    }
}
