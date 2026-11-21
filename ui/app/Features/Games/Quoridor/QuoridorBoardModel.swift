import Foundation

struct QuoridorBoardModel: Sendable, Equatable {
    let pawns: [QuoridorCell?]
    let walls: [QuoridorWall]
    let wallsLeft: [Int]
    let lastMove: QuoridorMove?
    let targets: Set<QuoridorCell>
    let legalWalls: Set<QuoridorWall>

    init(_ board: QuoridorBoardState, interactive: Bool) {
        pawns = (0..<2).map { seat in
            seat < board.pawns.count ? QuoridorCell(notation: board.pawns[seat]) : nil
        }
        walls = board.walls.compactMap { QuoridorWall(notation: $0) }
        wallsLeft = (0..<2).map { seat in
            seat < board.wallsLeft.count ? Int(board.wallsLeft[seat]) : Quoridor.wallsEach
        }
        lastMove = board.hasLastMove ? QuoridorMove(notation: board.lastMove) : nil
        let legal = interactive ? board.legalMoves.compactMap(QuoridorMove.init(notation:)) : []
        targets = Set(legal.compactMap { move in
            if case .pawn(let cell) = move { return cell }
            return nil
        })
        legalWalls = Set(legal.compactMap { move in
            if case .wall(let wall) = move { return wall }
            return nil
        })
    }

    func seat(at cell: QuoridorCell) -> Int? {
        pawns.firstIndex(of: cell)
    }

    func wallsLeft(seat: Int) -> Int {
        (0..<wallsLeft.count).contains(seat) ? wallsLeft[seat] : Quoridor.wallsEach
    }

    var lastCell: QuoridorCell? {
        if case .pawn(let cell) = lastMove { return cell }
        return nil
    }

    var lastWall: QuoridorWall? {
        if case .wall(let wall) = lastMove { return wall }
        return nil
    }
}
