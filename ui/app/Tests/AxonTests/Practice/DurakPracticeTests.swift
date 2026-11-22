import Foundation
import Testing
@testable import Axon

struct DurakPracticeTests {
    private func cards(_ raw: String) -> [String] {
        raw.split(separator: " ").map(String.init)
    }

    private struct BoutCase {
        let name: String
        let position: DurakPractice.Position
        let moves: [String]
        let legal: [String]
    }

    private func bout(_ attacker: String, _ defender: String, stock: String = "", limit: Int = 6) -> DurakPractice.Position {
        DurakPractice.Position(hands: [cards(attacker), cards(defender)], stock: cards(stock), trump: "6S", attacker: 0, boutLimit: limit)
    }

    private func run(_ position: DurakPractice.Position, _ moves: String...) throws -> DurakPractice.Position {
        try #require(PracticeReplay.play(DurakPractice.self, moves, from: position))
    }

    private func bot(_ position: DurakPractice.Position) -> String? {
        var random = ConstantRandom(0)
        return DurakPractice.botMove(position, level: .medium, using: &random)
    }

    @Test func beatsBySuitRankAndTrump() {
        let beating = [("KH", "7H"), ("6S", "AH"), ("7S", "6S")]
        let failing = [("7H", "KH"), ("AH", "6S"), ("6S", "7S"), ("AC", "6D")]
        for (defense, attack) in beating {
            #expect(DurakPractice.beats(defense, attack, trump: "S"), "\(defense) on \(attack)")
        }
        for (defense, attack) in failing {
            #expect(!DurakPractice.beats(defense, attack, trump: "S"), "\(defense) on \(attack)")
        }
    }

    @Test func dealsSixEachWithTheTrumpUnderTheStock() {
        for seed in UInt64(0)...20 {
            let position = DurakPractice.start(seed: seed)
            #expect(position.hands[0].count == 6)
            #expect(position.hands[1].count == 6)
            #expect(position.stockCount == 24)
            #expect(position.stock.last == position.trump)
            #expect(position.cards.sorted() == DurakPractice.deck().sorted())
            #expect(DurakPractice.turn(position) == position.attacker)
            #expect(position.boutLimit == 6)
        }
        #expect(DurakPractice.start(seed: 7) == DurakPractice.start(seed: 7))
        #expect(DurakPractice.start(seed: 7) != DurakPractice.start(seed: 8))
    }

    @Test func givesTheFirstAttackToTheLowestTrump() {
        #expect(DurakPractice.firstAttacker([cards("6S 7S"), cards("9H 6C")], trump: "H") == 1)
        #expect(DurakPractice.firstAttacker([cards("7D 8D"), cards("6D AD")], trump: "D") == 1)
        #expect(DurakPractice.firstAttacker([cards("6S 7S"), cards("8S 9S")], trump: "H") == 0)
        let ordered = DurakPractice.deck()
        #expect(DurakPractice.fromDeck(ordered).trump == ordered[12])
    }

    @Test func answersAnAttackWithDefencesOrATake() throws {
        let attacked = try run(bout("7H 9C", "8H AH 6C 7S", limit: 4), "attack 7H")
        #expect(DurakPractice.turn(attacked) == 1)
        #expect(DurakPractice.legalMoves(attacked) == ["defend 7H 8H", "defend 7H AH", "defend 7H 7S", "take"])
        #expect(DurakPractice.legalMoves(try run(attacked, "defend 7H 8H")) == ["pass"])
        #expect(DurakPractice.apply("defend 7H 6C", to: attacked) == nil)
    }

    @Test func limitsThrowIns() throws {
        let cases = [
            BoutCase(
                name: "rank on the table", position: bout("7H 7C 8D", "8H 9H TH", limit: 3),
                moves: ["attack 7H", "defend 7H 8H"], legal: ["attack 7C", "attack 8D", "pass"]
            ),
            BoutCase(
                name: "defender's starting hand caps the bout", position: bout("7H 7C 7D", "8H 8C", limit: 2),
                moves: ["attack 7H", "defend 7H 8H", "attack 7C", "defend 7C 8C"], legal: ["pass"]
            ),
            BoutCase(
                name: "throw-ins while taking", position: bout("7H 7C 7D", "8S 9S", limit: 2),
                moves: ["attack 7H", "take"], legal: ["attack 7C", "attack 7D", "pass"]
            ),
            BoutCase(
                name: "throw-ins on a take stop at the limit", position: bout("7H 7C 7D", "8S 9S", limit: 2),
                moves: ["attack 7H", "take", "attack 7C"], legal: ["pass"]
            ),
        ]
        for item in cases {
            let played = try #require(PracticeReplay.play(DurakPractice.self, item.moves, from: item.position), "\(item.name)")
            #expect(DurakPractice.legalMoves(played) == item.legal, "\(item.name)")
        }
    }

    @Test func discardsOnAPassAndHandsTheTableOverOnATakeRefillingTheAttackerFirst() throws {
        let passed = try run(bout("7H 9C", "8H AH", stock: "6C 6D 6H TC TD JD", limit: 2), "attack 7H", "defend 7H 8H", "pass")
        #expect(passed.discard == ["7H", "8H"])
        #expect(passed.attacker == 1)
        #expect(passed.hands[0] == cards("6C 9C TC 6D TD 6H"))
        #expect(passed.hands[1] == cards("JD AH"))
        let taken = try run(bout("7H 7C 9D", "8S AH", stock: "6C 6D", limit: 2), "attack 7H", "take", "attack 7C", "pass")
        #expect(taken.attacker == 0)
        #expect(taken.hands[1] == cards("7C 7H AH 8S"))
        #expect(taken.hands[0] == cards("6C 6D 9D"))
        #expect(taken.boutLimit == 4)
        #expect(taken.table.isEmpty)
        #expect(!taken.taking)
    }

    @Test func endsTheGame() throws {
        let draw = try run(bout("7H", "8H", limit: 1), "attack 7H", "defend 7H 8H", "pass")
        #expect(DurakPractice.outcome(draw) == .draw)
        #expect(draw.end == .draw)
        let attackerOut = try run(bout("7H", "6C", limit: 1), "attack 7H", "take", "pass")
        #expect(DurakPractice.outcome(attackerOut) == .won(seat: 0))
        #expect(attackerOut.end == .durak)
        let defenderOut = try run(bout("7H 9C", "8H", limit: 1), "attack 7H", "defend 7H 8H", "pass")
        #expect(DurakPractice.outcome(defenderOut) == .won(seat: 1))
        #expect(DurakPractice.legalMoves(defenderOut).isEmpty)
    }

    @Test func checksCards() {
        #expect(DurakPractice.isCard("TS"))
        #expect(!DurakPractice.isCard("1S"))
        #expect(!DurakPractice.isCard("TX"))
    }

    @Test func botPlaysLegalMovesUntilEveryGameEndsAndKeepsAllCards() {
        for seed in UInt64(1)...150 {
            var position = DurakPractice.start(seed: seed)
            var random = PracticeRandom(seed: seed)
            var plies = 0
            while DurakPractice.outcome(position) == nil, plies < 1000 {
                guard let move = DurakPractice.botMove(position, level: .medium, using: &random),
                      DurakPractice.legalMoves(position).contains(move),
                      let next = DurakPractice.apply(move, to: position)
                else {
                    Issue.record("seed \(seed): bot move rejected")
                    break
                }
                position = next
                plies += 1
                #expect(position.cards.sorted() == DurakPractice.deck().sorted())
            }
            #expect(DurakPractice.outcome(position) != nil)
        }
    }

    @Test func botBeatsCheaplyTakesWhenItCannotAndOpensWithItsLowestPlainCard() throws {
        #expect(bot(try run(bout("7H", "8H 6S 9H", limit: 3), "attack 7H")) == "defend 7H 8H")
        #expect(bot(try run(bout("AH", "8H 6C", limit: 2), "attack AH")) == "take")
        #expect(bot(bout("6S KC 9D", "7H")) == "attack 9D")
    }
}
