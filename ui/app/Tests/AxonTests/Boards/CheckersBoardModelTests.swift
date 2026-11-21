import Testing
@testable import Axon

struct CheckersBoardModelTests {
    private func squares(_ numbers: Int...) -> [CheckersSquare] {
        numbers.compactMap(CheckersSquare.init(number:))
    }

    @Test func marksMovablePiecesFromTheServerMoves() throws {
        let model = CheckersBoardModel(CheckersFixtures.opening, interactive: true)
        #expect(model.origins == Set(squares(9, 10, 11, 12)))
        #expect(model.targets.isEmpty)
        #expect(!model.forcedCapture)
        #expect(model.state(of: try CheckersFixtures.square(9)).spokenLabel == "9, black man, can move")
        #expect(model.state(of: try CheckersFixtures.square(13)).spokenLabel == "13, empty")
        #expect(model.state(of: try CheckersFixtures.square(22)).spokenLabel == "22, white man")
        #expect(model.moves(from: try CheckersFixtures.square(11)).map(\.notation) == ["11-15", "11-16"])
    }

    @Test func selectsMovesAndDeselects() throws {
        let idle = CheckersBoardModel(CheckersFixtures.opening, interactive: true)
        #expect(idle.tap(try CheckersFixtures.square(9)) == .select(squares(9)))
        #expect(idle.tap(try CheckersFixtures.square(22)) == .select([]))
        #expect(idle.tap(try CheckersFixtures.square(14)) == .select([]))

        let picked = CheckersBoardModel(CheckersFixtures.opening, interactive: true, path: squares(9))
        #expect(picked.targets == Set(squares(13, 14)))
        #expect(picked.state(of: try CheckersFixtures.square(9)).spokenLabel == "9, black man, selected")
        #expect(picked.state(of: try CheckersFixtures.square(14)).spokenLabel == "14, empty, move here")
        #expect(!picked.state(of: try CheckersFixtures.square(10)).movable)
        #expect(picked.tap(try CheckersFixtures.square(14)) == .move("9-14"))
        #expect(picked.tap(try CheckersFixtures.square(9)) == .select([]))
        #expect(picked.tap(try CheckersFixtures.square(10)) == .select(squares(10)))
        #expect(picked.tap(try CheckersFixtures.square(18)) == .select([]))
    }

    @Test func buildsAMultiJumpAsOneMove() throws {
        let board = CheckersFixtures.doubleKings
        let idle = CheckersBoardModel(board, interactive: true)
        #expect(idle.forcedCapture)
        #expect(idle.origins == Set(squares(14, 31)))
        #expect(idle.tap(try CheckersFixtures.square(31)) == .select(squares(31)))

        let origin = CheckersBoardModel(board, interactive: true, path: squares(31))
        #expect(origin.targets == Set(squares(22, 29, 13)))
        #expect(origin.tap(try CheckersFixtures.square(22)) == .select(squares(31, 22)))
        #expect(origin.tap(try CheckersFixtures.square(29)) == .move("31x22x29"))

        let midway = CheckersBoardModel(board, interactive: true, path: squares(31, 22))
        #expect(midway.targets == Set(squares(29, 13)))
        #expect(midway.state(of: try CheckersFixtures.square(22)).selected)
        #expect(midway.tap(try CheckersFixtures.square(13)) == .move("31x22x13"))

        let king = CheckersBoardModel(board, interactive: true, path: squares(14))
        #expect(king.tap(try CheckersFixtures.square(23)) == .move("14x21x30x23"))
        #expect(king.tap(try CheckersFixtures.square(21)) == .select(squares(14, 21)))
        let deeper = CheckersBoardModel(board, interactive: true, path: squares(14, 21))
        #expect(deeper.tap(try CheckersFixtures.square(30)) == .select(squares(14, 21, 30)))
        let last = CheckersBoardModel(board, interactive: true, path: squares(14, 21, 30))
        #expect(last.tap(try CheckersFixtures.square(23)) == .move("14x21x30x23"))
    }

    @Test func showsTheLastMoveAndNothingElseWhenIdle() throws {
        let model = CheckersBoardModel(CheckersFixtures.doubleKings, interactive: false, path: squares(31))
        #expect(model.moves.isEmpty)
        #expect(model.path.isEmpty)
        #expect(model.origins.isEmpty)
        #expect(model.trail == Set(squares(22, 17)))
        #expect(model.state(of: try CheckersFixtures.square(17)).spokenLabel == "17, white man, last move")
        #expect(model.state(of: try CheckersFixtures.square(31)).spokenLabel == "31, black king")
        #expect(model.tap(try CheckersFixtures.square(31)) == .select([]))
    }
}
