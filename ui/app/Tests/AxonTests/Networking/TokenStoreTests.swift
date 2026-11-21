import Foundation
import Security
import Testing
@testable import Axon

struct TokenStoreTests {
    @Test
    func inMemoryStoreRoundTrips() {
        let store = InMemoryTokenStore()
        #expect(store.load() == nil)
        let tokens = Fixtures.tokens("a-1")
        store.save(tokens)
        #expect(store.load() == tokens)
        store.clear()
        #expect(store.load() == nil)
    }

    @Test
    func vaultPersistsThroughTheStore() async {
        let store = InMemoryTokenStore()
        let vault = TokenVault(store: store)
        await vault.store(Fixtures.tokens("a-1"))
        #expect(store.load()?.accessToken == "a-1")
        #expect(await TokenVault(store: store).accessToken == "a-1")
        await vault.clear()
        #expect(store.load() == nil)
    }

    @Test
    func tokensFromAPairKeepThePreviousRefreshToken() throws {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let rotated = try #require(AuthTokens(Fixtures.pair("a-2", refresh: ""), previousRefreshToken: "r-1", now: now))
        #expect(rotated.refreshToken == "r-1")
        #expect(rotated.expiresAt == now.addingTimeInterval(900))
        #expect(AuthTokens(Fixtures.pair("", refresh: "r-1")) == nil)
        #expect(AuthTokens(Fixtures.pair("a-1", refresh: "")) == nil)
    }

    @Test
    func keychainAccountFollowsTheGateway() throws {
        let store = KeychainTokenStore(address: try GatewayAddress(parsing: "https://192.168.1.20:3443/"))
        #expect(store.account == "https://192.168.1.20:3443")
        #expect(store.service == "dev.axon.Axon.tokens")
    }

    #if os(macOS)
    @Test
    func keychainStoreRoundTripsAndCleansUp() throws {
        let store = KeychainTokenStore(
            service: "dev.axon.Axon.tests.\(UUID().uuidString)",
            account: "http://localhost:18080"
        )
        defer { try? store.clear() }
        #expect(try store.load() == nil)
        let first = Fixtures.tokens("a-1")
        try store.save(first)
        #expect(try store.load() == first)
        let second = Fixtures.tokens("a-2")
        try store.save(second)
        #expect(try store.load() == second)
        try store.clear()
        #expect(try store.load() == nil)
        try store.clear()
    }
    #endif
}
