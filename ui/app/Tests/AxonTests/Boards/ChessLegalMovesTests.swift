import Testing
@testable import Axon

struct ChessLegalMovesTests {
    private func square(_ name: String) throws -> ChessSquare {
        try #require(ChessSquare(name))
    }

    private func squares(_ names: String...) -> Set<ChessSquare> {
        Set(names.compactMap { ChessSquare($0) })
    }

    @Test func groupsTargetsByOrigin() throws {
        let legal = ChessLegalMoves(["e2e3", "e2e4", "g1f3", "g1h3", "b1c3"])
        #expect(legal.targets(from: try square("e2")) == squares("e3", "e4"))
        #expect(legal.targets(from: try square("g1")) == squares("f3", "h3"))
        #expect(legal.targets(from: try square("a2")).isEmpty)
        #expect(legal.hasMoves(from: try square("b1")))
        #expect(!legal.hasMoves(from: try square("d1")))
        #expect(legal.origins == squares("e2", "g1", "b1"))
    }

    @Test func groupsPromotionsByFromTo() throws {
        let legal = ChessLegalMoves(["e7e8q", "e7e8r", "e7e8b", "e7e8n", "e7d8n", "e7d8q", "a2a3"])
        let from = try square("e7")
        #expect(legal.targets(from: from) == squares("e8", "d8"))
        #expect(legal.candidates(from: from, to: try square("e8")).count == 4)
        #expect(legal.promotionChoices(from: from, to: try square("e8")) == [.queen, .rook, .bishop, .knight])
        #expect(legal.promotionChoices(from: from, to: try square("d8")) == [.queen, .knight])
        #expect(legal.promotionChoices(from: try square("a2"), to: try square("a3")).isEmpty)
    }

    @Test func ignoresMalformedMoves() throws {
        let legal = ChessLegalMoves(["", "e2", "e2e9", "z1a1", "e7e8x", "e2e4"])
        #expect(legal.origins == squares("e2"))
        #expect(legal.targets(from: try square("e2")) == squares("e4"))
    }

    @Test func parsesUCI() throws {
        let castle = try #require(ChessUCIMove("e1g1"))
        #expect(castle.from == (try square("e1")))
        #expect(castle.to == (try square("g1")))
        #expect(castle.promotion == nil)
        let promotion = try #require(ChessUCIMove("b7a8n"))
        #expect(promotion.promotion == .knight)
        #expect(ChessUCIMove.squares(of: "d1f3") == [try square("d1"), try square("f3")])
        #expect(ChessUCIMove.squares(of: "zz").isEmpty)
    }

    @Test func selectsPieceWithMoves() throws {
        let legal = ChessLegalMoves(["e2e4", "g1f3"])
        #expect(ChessTapOutcome.resolve(tap: try square("e2"), selected: nil, legal: legal) == .select(try square("e2")))
        #expect(ChessTapOutcome.resolve(tap: try square("d2"), selected: nil, legal: legal) == .select(nil))
        #expect(ChessTapOutcome.resolve(tap: try square("e2"), selected: try square("e2"), legal: legal) == .select(nil))
        #expect(ChessTapOutcome.resolve(tap: try square("g1"), selected: try square("e2"), legal: legal) == .select(try square("g1")))
        #expect(ChessTapOutcome.resolve(tap: try square("h8"), selected: try square("e2"), legal: legal) == .select(nil))
    }

    @Test func sendsMoveOnTarget() throws {
        let legal = ChessLegalMoves(["e2e4", "e1g1"])
        #expect(ChessTapOutcome.resolve(tap: try square("e4"), selected: try square("e2"), legal: legal) == .move("e2e4"))
        #expect(ChessTapOutcome.resolve(tap: try square("g1"), selected: try square("e1"), legal: legal) == .move("e1g1"))
    }

    @Test func asksForPromotionWhenMovesShareFromTo() throws {
        let legal = ChessLegalMoves(["a7a8q", "a7a8r", "a7a8b", "a7a8n", "a7b8q", "a7b8r", "a7b8b", "a7b8n"])
        let outcome = ChessTapOutcome.resolve(tap: try square("b8"), selected: try square("a7"), legal: legal)
        guard case .promote(let request) = outcome else {
            Issue.record("expected promotion, got \(outcome)")
            return
        }
        #expect(request.from == (try square("a7")))
        #expect(request.to == (try square("b8")))
        #expect(request.choices == [.queen, .rook, .bishop, .knight])
        #expect(request.move(.knight) == "a7b8n")
        #expect(request.move(.queen) == "a7b8q")
    }

    @Test func singlePromotionCandidateIsSentDirectly() throws {
        let legal = ChessLegalMoves(["a7a8q"])
        #expect(ChessTapOutcome.resolve(tap: try square("a8"), selected: try square("a7"), legal: legal) == .move("a7a8q"))
    }
}
