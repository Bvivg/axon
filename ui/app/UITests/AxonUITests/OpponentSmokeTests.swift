import XCTest

final class OpponentSmokeTests: XCTestCase {
    func testTwoOpponentsPlayTicTacToeUntilXWinsOnTheTopRow() async throws {
        let gateway = try E2EGateway.baseURL()
        let x = Opponent(baseURL: gateway)
        let o = Opponent(baseURL: gateway)
        try await x.register()
        try await o.register()
        try await o.login()

        let created = try await x.createSession(game: "tictactoe")
        XCTAssertEqual(created.status, .waiting)
        let joined = try await o.joinSession(created.id)
        XCTAssertEqual(joined.status, .playing)

        let xSocket = try await x.connect(to: created.id)
        let oSocket = try await o.connect(to: created.id)
        addTeardownBlock {
            await xSocket.close()
            await oSocket.close()
        }

        try await xSocket.play(cell: 0, atPly: 0)
        try await oSocket.play(cell: 3, atPly: 1)
        try await xSocket.play(cell: 1, atPly: 2)
        try await oSocket.play(cell: 4, atPly: 3)
        try await xSocket.play(cell: 2, atPly: 4)

        let seenByO = try await oSocket.nextSession("X to win") { $0.status == .won }
        let xID = await x.userID
        XCTAssertEqual(seenByO.winnerUserID, xID)
        XCTAssertEqual(seenByO.ply, 5)
        XCTAssertEqual(seenByO.ticTacToe.winningCells, [0, 1, 2])
        XCTAssertEqual(Array(seenByO.ticTacToe.cells.prefix(5)), [.x, .x, .x, .o, .o])

        let fetched = try await o.getSession(created.id)
        XCTAssertEqual(fetched.status, .won)
        XCTAssertEqual(fetched.winnerUserID, xID)
    }
}
