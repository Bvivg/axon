import Foundation
import Testing
@testable import Axon

struct SessionCodecTests {
    let codec = SessionCodec()

    static func command(_ action: SessionCommand.OneOf_Action) -> SessionCommand {
        var command = SessionCommand()
        command.action = action
        return command
    }

    static func ticTacToe(ply: UInt32, cell: UInt32, revision: UInt32?) -> SessionCommand.OneOf_Action {
        var move = Axon_Game_V1_TicTacToeMove()
        move.ply = ply
        move.cell = cell
        if let revision { move.revision = revision }
        return .move(move)
    }

    static func chess(ply: UInt32, uci: String, revision: UInt32?) -> SessionCommand.OneOf_Action {
        var move = Axon_Game_V1_ChessMove()
        move.ply = ply
        move.uci = uci
        if let revision { move.revision = revision }
        return .chessMove(move)
    }

    static func quoridor(ply: UInt32, move text: String, revision: UInt32?) -> SessionCommand.OneOf_Action {
        var move = Axon_Game_V1_QuoridorMove()
        move.ply = ply
        move.move = text
        if let revision { move.revision = revision }
        return .quoridorMove(move)
    }

    static func checkers(ply: UInt32, move text: String, revision: UInt32?) -> SessionCommand.OneOf_Action {
        var move = Axon_Game_V1_CheckersMove()
        move.ply = ply
        move.move = text
        if let revision { move.revision = revision }
        return .checkersMove(move)
    }

    static func draw(_ action: DrawAction) -> SessionCommand.OneOf_Action {
        var draw = Axon_Game_V1_DrawCommand()
        draw.action = action
        return .draw(draw)
    }

    static func undo(revision: UInt32?) -> SessionCommand.OneOf_Action {
        var undo = Axon_Game_V1_UndoMove()
        if let revision { undo.revision = revision }
        return .undo(undo)
    }

    static let exactFrames: [(SessionCommand, String)] = [
        (
            SessionCodec.subscribe(.session("0b9f6a52-0c33-4b1e-9d0e-5f1f3c7a8e21")),
            #"{"subscribe":"0b9f6a52-0c33-4b1e-9d0e-5f1f3c7a8e21"}"#
        ),
        (SessionCodec.subscribe(.match(.chess)), #"{"subscribeMatch":"chess"}"#),
        (SessionCodec.subscribe(.match(.ticTacToe)), #"{"subscribeMatch":"tictactoe"}"#),
        (command(ticTacToe(ply: 3, cell: 4, revision: 7)), #"{"move":{"ply":3,"cell":4,"revision":7}}"#),
        (command(ticTacToe(ply: 0, cell: 0, revision: 0)), #"{"move":{"revision":0}}"#),
        (command(chess(ply: 2, uci: "e7e8q", revision: 9)), #"{"chessMove":{"ply":2,"uci":"e7e8q","revision":9}}"#),
        (command(quoridor(ply: 5, move: "e3h", revision: nil)), #"{"quoridorMove":{"ply":5,"move":"e3h"}}"#),
        (command(quoridor(ply: 6, move: "d4", revision: 11)), #"{"quoridorMove":{"ply":6,"move":"d4","revision":11}}"#),
        (SessionCodec.subscribe(.match(.checkers)), #"{"subscribeMatch":"checkers"}"#),
        (command(checkers(ply: 4, move: "22x15x6", revision: 9)), #"{"checkersMove":{"ply":4,"move":"22x15x6","revision":9}}"#),
        (command(checkers(ply: 0, move: "11-15", revision: nil)), #"{"checkersMove":{"move":"11-15"}}"#),
        (command(draw(.offer)), #"{"draw":{"action":"DRAW_ACTION_OFFER"}}"#),
        (command(draw(.accept)), #"{"draw":{"action":"DRAW_ACTION_ACCEPT"}}"#),
        (command(draw(.decline)), #"{"draw":{"action":"DRAW_ACTION_DECLINE"}}"#),
        (command(undo(revision: 4)), #"{"undo":{"revision":4}}"#),
        (command(undo(revision: nil)), #"{"undo":{}}"#),
    ]

    @Test(arguments: exactFrames)
    func encodesExactFrames(command: SessionCommand, frame: String) throws {
        #expect(try codec.encode(command) == frame)
    }

    @Test(arguments: exactFrames.map(\.0))
    func commandsRoundTrip(command: SessionCommand) throws {
        let decoded = try SessionCommand(jsonString: codec.encode(command))
        #expect(decoded == command)
    }

    @Test
    func sessionEventsRoundTrip() throws {
        var session = Fixtures.session("s-1", revision: 4)
        session.ply = 4
        session.turnUserID = "u-1"
        session.ticTacToe.cells = [.x, .o, .unspecified, .x, .unspecified, .unspecified, .o, .unspecified, .unspecified]
        session.clock.xRemainingMs = 290_000
        session.clock.oRemainingMs = 281_500
        var event = SessionEvent()
        event.session = session
        let decoded = try codec.decode(event.jsonString())
        #expect(decoded == event)
        #expect(codec.event(from: try event.jsonString()) == .session(session))
    }

    @Test
    func decodesServerFramesAndIgnoresUnknownFields() throws {
        let frame = """
        {"session":{"id":"s-9","game":"chess","playerIds":["u-1","u-2"],"status":"SESSION_STATUS_PLAYING",\
        "ply":12,"turnUserId":"u-2","chess":{"fen":"8/8/8/8/8/8/8/K6k w - - 0 1","san":["e4"],"legalMoves":["a1a2"],\
        "check":true,"futureBoardField":1},"updatedAt":"2026-11-21T10:15:30.250Z","revision":13,"spectators":2,\
        "futureField":{"nested":[1,2,3]}},"futureUpdate":"ignored"}
        """
        let event = try codec.decode(frame)
        guard case .session(let session) = event.update else {
            Issue.record("expected a session update")
            return
        }
        #expect(session.id == "s-9")
        #expect(session.kind == .chess)
        #expect(session.status == .playing)
        #expect(session.turnUserID == "u-2")
        #expect(session.revisionValue == 13)
        #expect(session.chess.check)
        #expect(session.chess.legalMoves == ["a1a2"])
        #expect(session.spectators == 2)
    }

    @Test
    func decodesErrorsAndMatches() {
        #expect(codec.event(from: #"{"error":"SESSION_ERROR_NOT_YOUR_TURN"}"#) == .error(.notYourTurn))
        var ticket = MatchTicket()
        ticket.game = "chess"
        ticket.status = .matched
        ticket.sessionID = "s-3"
        ticket.rating = 1500
        let frame = #"{"match":{"game":"chess","status":"MATCH_STATUS_MATCHED","rating":1500,"sessionId":"s-3"}}"#
        #expect(codec.event(from: frame) == .match(ticket))
    }

    @Test(arguments: ["", "not json", "{}", #"{"futureUpdate":{"a":1}}"#, #"{"session":"wrong"}"#])
    func skipsUnreadableFrames(frame: String) {
        #expect(codec.event(from: frame) == nil)
    }
}
