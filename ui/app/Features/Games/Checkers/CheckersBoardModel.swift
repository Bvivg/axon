import Foundation

enum CheckersTapOutcome: Sendable, Equatable {
    case select([CheckersSquare])
    case move(String)
}

struct CheckersSquareState: Sendable, Equatable {
    let square: CheckersSquare
    let piece: CheckersPiece?
    let selected: Bool
    let movable: Bool
    let target: Bool
    let lastMove: Bool

    var spokenLabel: String {
        var parts = ["\(square.number)", piece?.label ?? "empty"]
        if selected {
            parts.append("selected")
        } else if movable {
            parts.append("can move")
        }
        if target { parts.append("move here") }
        if lastMove { parts.append("last move") }
        return parts.joined(separator: ", ")
    }
}

struct CheckersBoardModel: Sendable, Equatable {
    let position: CheckersPosition
    let moves: [CheckersMove]
    let path: [CheckersSquare]
    let trail: Set<CheckersSquare>
    let origins: Set<CheckersSquare>
    let targets: Set<CheckersSquare>

    init(_ board: CheckersBoardState, interactive: Bool, path: [CheckersSquare] = []) {
        position = CheckersPosition(board.squares)
        let moves = interactive ? board.legalMoves.compactMap(CheckersMove.init(notation:)) : []
        self.moves = moves
        let active = moves.isEmpty ? [] : path
        self.path = active
        trail = Set(board.hasLastMove ? CheckersMove(notation: board.lastMove)?.path ?? [] : [])
        origins = Set(moves.map(\.origin))
        let matching = moves.filter { $0.path.starts(with: active) && $0.path.count > active.count }
        targets = active.isEmpty ? [] : Set(matching.flatMap { [$0.path[active.count], $0.destination] })
    }

    var forcedCapture: Bool { moves.contains(where: \.jump) }

    func state(of square: CheckersSquare) -> CheckersSquareState {
        CheckersSquareState(
            square: square,
            piece: position[square],
            selected: path.contains(square),
            movable: path.isEmpty && origins.contains(square),
            target: targets.contains(square) && path != [square],
            lastMove: trail.contains(square)
        )
    }

    func moves(from square: CheckersSquare) -> [CheckersMove] {
        moves.filter { $0.origin == square }
    }

    func tap(_ square: CheckersSquare) -> CheckersTapOutcome {
        if path == [square] { return .select([]) }
        if !path.isEmpty, let outcome = continuation(tapping: square) {
            return outcome
        }
        return .select(origins.contains(square) ? [square] : [])
    }

    private func continuation(tapping square: CheckersSquare) -> CheckersTapOutcome? {
        let matching = moves.filter { $0.path.starts(with: path) && $0.path.count > path.count }
        let next = matching.filter { $0.path[path.count] == square }
        let exact = next.filter { $0.path.count == path.count + 1 }
        let longer = next.filter { $0.path.count > path.count + 1 }
        if exact.count == 1, longer.isEmpty, let only = exact.first {
            return .move(only.notation)
        }
        if !longer.isEmpty {
            return .select(path + [square])
        }
        let ending = matching.filter { $0.destination == square }
        if ending.count == 1, let only = ending.first {
            return .move(only.notation)
        }
        return nil
    }
}
