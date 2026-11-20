import Testing
@testable import Axon

struct ChessPositionTests {
    private func square(_ name: String) throws -> ChessSquare {
        try #require(ChessSquare(name))
    }

    @Test func parsesStartPosition() throws {
        let position = ChessPosition(fen: "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")
        #expect(position[try square("a1")] == ChessPiece(kind: .rook, side: .white))
        #expect(position[try square("e1")] == ChessPiece(kind: .king, side: .white))
        #expect(position[try square("d8")] == ChessPiece(kind: .queen, side: .black))
        #expect(position[try square("g8")] == ChessPiece(kind: .knight, side: .black))
        #expect(position[try square("e2")] == ChessPiece(kind: .pawn, side: .white))
        #expect(position[try square("e4")] == nil)
        #expect(position.squares.compactMap { $0 }.count == 32)
        #expect(position.sideToMove == .white)
        #expect(position.castling == [.whiteKingside, .whiteQueenside, .blackKingside, .blackQueenside])
        #expect(position.enPassant == nil)
        #expect(position.halfmoveClock == 0)
        #expect(position.fullmoveNumber == 1)
    }

    @Test func parsesSideToMoveAndEnPassant() throws {
        let position = ChessPosition(fen: "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1")
        #expect(position.sideToMove == .black)
        #expect(position.enPassant == (try square("e3")))
        #expect(position[try square("e4")] == ChessPiece(kind: .pawn, side: .white))
        #expect(position[try square("e2")] == nil)
    }

    @Test(arguments: [
        ("KQkq", ChessCastling([.whiteKingside, .whiteQueenside, .blackKingside, .blackQueenside])),
        ("Kq", ChessCastling([.whiteKingside, .blackQueenside])),
        ("Qk", ChessCastling([.whiteQueenside, .blackKingside])),
        ("-", ChessCastling()),
    ])
    func parsesCastling(field: String, expected: ChessCastling) {
        let position = ChessPosition(fen: "r3k2r/8/8/8/8/8/8/R3K2R w \(field) - 4 20")
        #expect(position.castling == expected)
        #expect(position.halfmoveClock == 4)
        #expect(position.fullmoveNumber == 20)
    }

    @Test func toleratesEmptyAndShortFEN() {
        let empty = ChessPosition(fen: "")
        #expect(empty.squares.allSatisfy { $0 == nil })
        #expect(empty.sideToMove == .white)
        #expect(empty.castling.isEmpty)
        #expect(empty.enPassant == nil)

        let placementOnly = ChessPosition(fen: "8/8/8/8/8/8/8/4K3")
        #expect(placementOnly.king(of: .white) == ChessSquare("e1"))
        #expect(placementOnly.king(of: .black) == nil)
    }

    @Test func findsKingInCheckPosition() throws {
        let position = ChessPosition(fen: "r1bq1b1r/ppp2kpp/2n5/3np3/2B5/5Q2/PPPP1PPP/RNB1K2R b KQ - 1 7")
        #expect(position.sideToMove == .black)
        #expect(position.king(of: .black) == (try square("f7")))
        #expect(position.king(of: .white) == (try square("e1")))
        #expect(position.castling == [.whiteKingside, .whiteQueenside])
        #expect(position[try square("f3")] == ChessPiece(kind: .queen, side: .white))
    }

    @Test(arguments: [
        ("a1", 0, 0), ("e4", 4, 3), ("h8", 7, 7),
    ])
    func parsesSquares(name: String, file: Int, rank: Int) throws {
        let parsed = try square(name)
        #expect(parsed.file == file)
        #expect(parsed.rank == rank)
        #expect(parsed.name == name)
    }

    @Test(arguments: ["", "e", "i1", "a9", "a0", "e44", "E4"])
    func rejectsBadSquares(name: String) {
        #expect(ChessSquare(name) == nil)
    }

    @Test(arguments: [
        (0, 0, 0, "a8"), (0, 7, 0, "a1"), (0, 7, 7, "h1"), (0, 0, 7, "h8"),
        (1, 0, 0, "h1"), (1, 7, 0, "h8"), (1, 7, 7, "a8"), (1, 0, 7, "a1"),
    ])
    func orientsByPerspective(perspective: Int, row: Int, column: Int, expected: String) {
        #expect(ChessSquare.at(row: row, column: column, perspective: perspective)?.name == expected)
    }

    @Test func colorsSquares() throws {
        #expect(try square("a1").isDark)
        #expect(try !square("h1").isDark)
        #expect(try square("h8").isDark)
    }
}
