import Foundation
import Testing
@testable import Axon

enum CheckersFixtures {
    static let codes: [Character: CheckersPieceCode] = [
        ".": .unspecified, "b": .blackMan, "B": .blackKing, "w": .whiteMan, "W": .whiteKing,
    ]

    static func board(_ squares: String, legal: [String] = [], moves: [String] = [], last: String? = nil) -> CheckersBoardState {
        CheckersBoardState.with {
            $0.squares = squares.map { codes[$0] ?? .unspecified }
            $0.legalMoves = legal
            $0.moves = moves
            if let last { $0.lastMove = last }
        }
    }

    static let opening = board(
        "bbbbbbbbbbbb........wwwwwwwwwwww",
        legal: ["9-13", "9-14", "10-14", "10-15", "11-15", "11-16", "12-16"]
    )

    static let doubleKings = board(
        "bbbbbbbb.....B.bw.......ww.w..Bw",
        legal: ["14x21x30x23", "31x22x29", "31x22x13"],
        moves: [
            "9-14", "23-18", "14x23", "26x19", "11-16", "30-26", "16x23x30", "21-17",
            "30x21x14", "31-26", "10-15", "24-19", "15x24x31", "29-25", "12-16", "22-17",
        ],
        last: "22-17"
    )

    static func square(_ number: Int) throws -> CheckersSquare {
        try #require(CheckersSquare(number: number))
    }
}

struct CheckersNotationTests {
    @Test(arguments: [(1, 0, 1), (4, 0, 7), (5, 1, 0), (14, 3, 2), (29, 7, 0), (32, 7, 6)])
    func mapsSquareNumbersToTheBoard(number: Int, row: Int, col: Int) throws {
        let square = try CheckersFixtures.square(number)
        #expect(square.row == row)
        #expect(square.col == col)
        #expect(CheckersSquare(row: row, col: col) == square)
    }

    @Test func coversEveryDarkSquareOnce() {
        let squares = (0..<8).flatMap { row in (0..<8).compactMap { CheckersSquare(row: row, col: $0) } }
        #expect(squares.count == 32)
        #expect(squares.map(\.number) == Array(1...32))
        #expect(CheckersSquare(row: 0, col: 0) == nil)
        #expect(CheckersSquare(number: 0) == nil)
        #expect(CheckersSquare(number: 33) == nil)
    }

    @Test(arguments: [
        (0, 7, 0, 4), (0, 0, 7, 29), (0, 0, 1, 32), (0, 7, 6, 1),
        (1, 0, 1, 1), (1, 7, 0, 29), (1, 0, 7, 4), (1, 7, 6, 32),
    ])
    func flipsForTheSecondPlayer(perspective: Int, displayRow: Int, displayCol: Int, number: Int) throws {
        let square = try CheckersFixtures.square(number)
        #expect(CheckersSquare.at(displayRow: displayRow, displayCol: displayCol, perspective: perspective) == square)
        let display = square.display(perspective: perspective)
        #expect(display.row == displayRow)
        #expect(display.col == displayCol)
        #expect(CheckersSquare.at(displayRow: displayRow, displayCol: displayCol + 1, perspective: perspective) == nil)
    }

    @Test(arguments: [
        ("11-15", [11, 15], false),
        ("9-5", [9, 5], false),
        ("22x15x6", [22, 15, 6], true),
        ("14x21x30x23", [14, 21, 30, 23], true),
        ("32x23", [32, 23], true),
    ])
    func parsesAndFormatsMoves(notation: String, numbers: [Int], jump: Bool) throws {
        let move = try #require(CheckersMove(notation: notation))
        #expect(move.path.map(\.number) == numbers)
        #expect(move.jump == jump)
        #expect(move.notation == notation)
        #expect(move.origin.number == numbers.first)
        #expect(move.destination.number == numbers.last)
        let rebuilt = try #require(CheckersMove(path: numbers.compactMap(CheckersSquare.init(number:)), jump: jump))
        #expect(rebuilt == move)
    }

    @Test(arguments: [
        "", "1", "11", "11-", "-15", "11-15-19", "11x15-19", "11--15", "0-5", "33-28", "05-9",
        "1x", "x1x5", "+1-5", "a-b", "11 15", "1-2x3", "١١-١٥",
        String(repeating: "1x", count: 13) + "1",
        String(repeating: "10x", count: 13) + "10",
    ])
    func rejectsMalformedMoves(notation: String) {
        #expect(CheckersMove(notation: notation) == nil)
    }

    @Test func acceptsTheLongestSeries() throws {
        let longest = String(repeating: "1x", count: 12) + "1"
        let move = try #require(CheckersMove(notation: longest))
        #expect(move.path.count == 13)
        #expect(CheckersMove(path: Array(repeating: try CheckersFixtures.square(1), count: 2), jump: false) != nil)
        #expect(CheckersMove(path: [try CheckersFixtures.square(1)], jump: true) == nil)
        #expect(CheckersMove(path: (1...3).compactMap(CheckersSquare.init(number:)), jump: false) == nil)
    }

    @Test func speaksMovesForVoiceOver() throws {
        #expect(try #require(CheckersMove(notation: "11-15")).spokenName == "Move 11 to 15")
        #expect(try #require(CheckersMove(notation: "22x15x6")).spokenName == "Capture 22 to 15 to 6")
    }
}

struct CheckersPositionTests {
    @Test func decodesPiecesFromTheProtoBoard() throws {
        let position = CheckersPosition(CheckersFixtures.doubleKings.squares)
        #expect(position[try CheckersFixtures.square(1)] == CheckersPiece(side: .black, isKing: false))
        #expect(position[try CheckersFixtures.square(14)] == CheckersPiece(side: .black, isKing: true))
        #expect(position[try CheckersFixtures.square(17)] == CheckersPiece(side: .white, isKing: false))
        #expect(position[try CheckersFixtures.square(31)]?.label == "black king")
        #expect(position[try CheckersFixtures.square(9)] == nil)
        #expect(position.count(.black) == 11)
        #expect(position.count(.white) == 5)
        let kings = CheckersPosition(CheckersFixtures.board("W" + String(repeating: ".", count: 31)).squares)
        #expect(kings[try CheckersFixtures.square(1)]?.label == "white king")
    }

    @Test func fallsBackToTheStartForMissingBoards() {
        let empty = CheckersPosition([])
        #expect(empty.count(.black) == 12)
        #expect(empty.count(.white) == 12)
        #expect(empty == CheckersPosition(CheckersFixtures.opening.squares))
        let unknown = CheckersPosition(Array(repeating: .UNRECOGNIZED(9), count: 32))
        #expect(unknown.squares.allSatisfy { $0 == nil })
    }

    @Test func decodesTheServerFrame() throws {
        let names = ["CHECKERS_PIECE_BLACK_MAN", "CHECKERS_PIECE_BLACK_KING", "CHECKERS_PIECE_UNSPECIFIED", "CHECKERS_PIECE_WHITE_KING"]
        let squares = (0..<32).map { names[$0 % 4] }.map { "\"\($0)\"" }.joined(separator: ",")
        let frame = """
        {"session":{"id":"s-c","game":"checkers","playerIds":["u-1","u-2"],"status":"SESSION_STATUS_DRAW","ply":80,\
        "checkers":{"squares":[\(squares)],"moves":["11-15","22x15x6"],"lastMove":"22x15x6","end":"CHECKERS_END_FORTY_MOVES",\
        "quietPlies":80,"futureField":true}}}
        """
        let event = try SessionCodec().decode(frame)
        guard case .session(let session) = event.update else {
            Issue.record("expected a session update")
            return
        }
        #expect(session.kind == .checkers)
        #expect(session.isSupported)
        #expect(session.checkers.end == .fortyMoves)
        #expect(session.checkers.quietPlies == 80)
        let position = CheckersPosition(session.checkers.squares)
        #expect(position[try CheckersFixtures.square(2)] == CheckersPiece(side: .black, isKing: true))
        #expect(position[try CheckersFixtures.square(3)] == nil)
        #expect(position[try CheckersFixtures.square(4)] == CheckersPiece(side: .white, isKing: true))
        #expect(position.count(.black) == 16)
        #expect(SessionStatusText.detail(session) == "forty-move rule")
    }
}
