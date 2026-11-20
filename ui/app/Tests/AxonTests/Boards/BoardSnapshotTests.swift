import Foundation
import ImageIO
import SwiftUI
import Testing
import UniformTypeIdentifiers
@testable import Axon

@MainActor
struct BoardSnapshotTests {
    static let sides: [CGFloat] = [402, 700]
    static let schemes: [ColorScheme] = [.light, .dark]

    static var directory: URL {
        var url = URL(fileURLWithPath: #filePath)
        for _ in 0..<6 { url.deleteLastPathComponent() }
        return url.appending(path: ".local/app/snapshots", directoryHint: .isDirectory)
    }

    static func wonTicTacToe() -> TicTacToeBoardState {
        var board = TicTacToeBoardState()
        board.cells = [.x, .o, .o, .unspecified, .x, .unspecified, .o, .unspecified, .x]
        board.winningCells = [0, 4, 8]
        return board
    }

    static func checkedChess(interactive: Bool) -> ChessBoardState {
        var board = ChessBoardState()
        board.fen = "r1bq1b1r/ppp2kpp/2n5/3np3/2B5/5Q2/PPPP1PPP/RNB1K2R b KQ - 1 7"
        board.san = ["e4", "e5", "Nf3", "Nc6", "Bc4", "Nf6", "Ng5", "d5", "exd5", "Nxd5", "Nxf7", "Kxf7", "Qf3+"]
        board.lastMove = "d1f3"
        board.check = true
        board.legalMoves = interactive ? ["f7e6", "f7e7", "f7e8", "f7g6", "f7g8", "d8f6"] : []
        return board
    }

    static func promotionChess() -> ChessBoardState {
        var board = ChessBoardState()
        board.fen = "1r4k1/P4ppp/8/8/8/8/5PPP/6K1 w - - 0 32"
        board.san = ["Rb8"]
        board.lastMove = "b2b8"
        board.legalMoves = ["a7a8q", "a7a8r", "a7a8b", "a7a8n", "a7b8q", "a7b8r", "a7b8b", "a7b8n", "g1f1", "h2h3"]
        return board
    }

    static func midGameQuoridor() -> QuoridorBoardState {
        let placed = ["d6h", "e4h", "c5v", "f6v", "b3h", "g3h"]
        var board = QuoridorBoardState()
        board.pawns = ["e4", "e6"]
        board.walls = placed
        board.wallsLeft = [7, 7]
        board.moves = ["e2", "e8", "e3", "e7", "d6h", "e6", "e4", "c5v", "b3h", "f6v", "g3h", "e4h"]
        board.lastMove = "e4h"
        board.legalMoves = ["d4", "f4", "e3"] + openWalls(besides: placed).map(\.notation)
        return board
    }

    static func openWalls(besides placed: [String]) -> [QuoridorWall] {
        let taken = placed.compactMap { QuoridorWall(notation: $0) }
        return QuoridorNotationTests.allWalls.filter { wall in
            !taken.contains { other in
                if other.row == wall.row && other.col == wall.col { return true }
                guard other.orientation == wall.orientation else { return false }
                switch wall.orientation {
                case .horizontal: return other.row == wall.row && abs(other.col - wall.col) == 1
                case .vertical: return other.col == wall.col && abs(other.row - wall.row) == 1
                }
            }
        }
    }

    static func write(_ view: some View, name: String, size: CGSize, scheme: ColorScheme) throws -> URL {
        let content = view
            .frame(width: size.width, height: size.height)
            .background(.background)
            .environment(\.colorScheme, scheme)
        let renderer = ImageRenderer(content: content)
        renderer.scale = 2
        renderer.proposedSize = ProposedViewSize(size)
        let image = try #require(renderer.cgImage)
        #expect(image.width == Int(size.width * 2))
        #expect(image.height == Int(size.height * 2))
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let url = directory.appending(path: "\(name)-\(Int(size.width))-\(scheme == .dark ? "dark" : "light").png")
        let destination = try #require(CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil))
        CGImageDestinationAddImage(destination, image, nil)
        #expect(CGImageDestinationFinalize(destination))
        return url
    }

    @Test func rendersTicTacToe() throws {
        for side in Self.sides {
            for scheme in Self.schemes {
                _ = try Self.write(
                    TicTacToeBoardView(board: Self.wonTicTacToe(), interactive: false, onTap: { _ in }).padding(16),
                    name: "tictactoe-won",
                    size: CGSize(width: side, height: side),
                    scheme: scheme
                )
            }
        }
    }

    @Test func rendersChess() throws {
        for side in Self.sides {
            for scheme in Self.schemes {
                _ = try Self.write(
                    ChessBoardView(
                        board: Self.checkedChess(interactive: true),
                        perspective: 1,
                        interactive: true,
                        selection: ChessSquare("f7"),
                        promotion: nil,
                        onMove: { _ in }
                    ),
                    name: "chess-check",
                    size: CGSize(width: side, height: side),
                    scheme: scheme
                )
                _ = try Self.write(
                    ChessBoardView(board: Self.checkedChess(interactive: false), perspective: 0, interactive: false, onMove: { _ in }),
                    name: "chess-spectator",
                    size: CGSize(width: side, height: side),
                    scheme: scheme
                )
            }
        }
        let promotion = ChessLegalMoves(Self.promotionChess().legalMoves)
        let from = try #require(ChessSquare("a7"))
        let to = try #require(ChessSquare("b8"))
        for scheme in Self.schemes {
            _ = try Self.write(
                ChessBoardView(
                    board: Self.promotionChess(),
                    perspective: 0,
                    interactive: true,
                    selection: nil,
                    promotion: ChessPromotionRequest(from: from, to: to, choices: promotion.promotionChoices(from: from, to: to)),
                    onMove: { _ in }
                ),
                name: "chess-promotion",
                size: CGSize(width: 402, height: 402),
                scheme: scheme
            )
        }
    }

    @Test func rendersQuoridor() throws {
        let pending = try #require(QuoridorWall(notation: "c2h"))
        for side in Self.sides {
            for scheme in Self.schemes {
                _ = try Self.write(
                    QuoridorBoardView(
                        board: Self.midGameQuoridor(),
                        perspective: 0,
                        interactive: true,
                        placement: WallPlacementState(pending: pending),
                        onMove: { _ in }
                    ),
                    name: "quoridor-violet",
                    size: CGSize(width: side, height: side),
                    scheme: scheme
                )
                _ = try Self.write(
                    QuoridorBoardView(board: Self.midGameQuoridor(), perspective: 1, interactive: false, onMove: { _ in }),
                    name: "quoridor-green",
                    size: CGSize(width: side, height: side),
                    scheme: scheme
                )
            }
        }
    }

    @Test func rendersClocksAndMoves() throws {
        let now = Date()
        for scheme in Self.schemes {
            _ = try Self.write(
                VStack(alignment: .leading, spacing: 12) {
                    HStack(spacing: 12) {
                        GameClockView(remaining: .seconds(312), running: true, sampledAt: now)
                        GameClockView(remaining: .seconds(487), running: false, sampledAt: now)
                        GameClockView(remaining: .milliseconds(7_400), running: true, sampledAt: now)
                    }
                    HStack(alignment: .top, spacing: 12) {
                        MoveListContent(moves: Self.checkedChess(interactive: false).san, highlightedPly: 13, layout: .pairs)
                        MoveListContent(moves: Array(Self.midGameQuoridor().moves.prefix(7)), highlightedPly: 5, layout: .plain)
                    }
                    MoveListContent(moves: [], highlightedPly: nil, layout: .pairs)
                    Spacer(minLength: 0)
                }
                .padding(16),
                name: "clock-moves",
                size: CGSize(width: 402, height: 402),
                scheme: scheme
            )
        }
    }
}
