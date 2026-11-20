import Foundation
import Testing
@testable import Axon

struct GatewayAddressTests {
    @Test(arguments: [
        ("http://localhost:18080", "http://localhost:18080", "ws://localhost:18080/ws/game"),
        ("  https://axon.test/ ", "https://axon.test", "wss://axon.test/ws/game"),
        ("HTTP://127.0.0.1:18680", "http://127.0.0.1:18680", "ws://127.0.0.1:18680/ws/game"),
        ("http://[::1]:18080", "http://[::1]:18080", "ws://[::1]:18080/ws/game"),
        ("https://192.168.1.20:3443", "https://192.168.1.20:3443", "wss://192.168.1.20:3443/ws/game"),
    ])
    func parsesAndDerivesSocket(input: String, host: String, socket: String) throws {
        let address = try GatewayAddress(parsing: input)
        #expect(address.host == host)
        #expect(address.gameSocketURL.absoluteString == socket)
    }

    @Test(arguments: [
        ("", GatewayAddressError.empty),
        ("ftp://localhost", .unsupportedScheme),
        ("localhost:18080", .unsupportedScheme),
        ("http://", .missingHost),
        ("http://localhost:18080/api", .hasPath),
        ("http://localhost:18080?x=1", .hasPath),
        ("http://192.168.1.20:18680", .insecureRemote),
        ("http://axon.test", .insecureRemote),
        ("http://127.0.0.1.evil.test", .insecureRemote),
    ])
    func rejectsInvalid(input: String, expected: GatewayAddressError) {
        #expect(throws: expected) { try GatewayAddress(parsing: input) }
    }
}
