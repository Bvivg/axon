import Connect
import Foundation
import Testing
@testable import Axon

@Suite(.timeLimit(.minutes(1)))
struct LiveAuthAPITests {
    let client = ScriptedAuthClient()

    func api(_ vault: TokenVault) -> LiveAuthAPI {
        LiveAuthAPI(client: client, vault: vault)
    }

    func serveLogin(_ user: User, pair: TokenPair) {
        client.script.on("Login") { (_: String?) -> Result<Axon_Auth_V1_LoginResponse, ConnectError> in
            var response = Axon_Auth_V1_LoginResponse()
            response.user = user
            response.tokens = pair
            return .success(response)
        }
    }

    func serveMe(acceptedToken: String, user: User) {
        client.script.on("GetMe") { (authorization: String?) -> Result<Axon_Auth_V1_GetMeResponse, ConnectError> in
            guard authorization == "Bearer \(acceptedToken)" else { return .failure(Fixtures.denied(.unauthenticated)) }
            var response = Axon_Auth_V1_GetMeResponse()
            response.user = user
            return .success(response)
        }
    }

    @Test
    func loginStoresTheIssuedTokens() async throws {
        serveLogin(Fixtures.user("u-1"), pair: Fixtures.pair("a-1", refresh: "r-1"))
        let vault = Fixtures.vault(nil)
        let user = try await api(vault).login(email: "u-1@axon.test", password: "secret-123")
        #expect(user.id == "u-1")
        #expect(await vault.accessToken == "a-1")
        #expect(await vault.refreshToken == "r-1")
        let call = try #require(client.script.recorded("Login").first)
        #expect(call.authorization == nil)
        let request = try #require(call.request as? Axon_Auth_V1_LoginRequest)
        #expect(request.email == "u-1@axon.test")
        #expect(request.password == "secret-123")
    }

    @Test
    func registerSendsTheUsernameAndStoresTheTokens() async throws {
        client.script.on("Register") { (_: String?) -> Result<Axon_Auth_V1_RegisterResponse, ConnectError> in
            var response = Axon_Auth_V1_RegisterResponse()
            response.user = Fixtures.user("u-2")
            response.tokens = Fixtures.pair("a-2", refresh: "r-2")
            return .success(response)
        }
        let vault = Fixtures.vault(nil)
        let user = try await api(vault).register(email: "u-2@axon.test", username: "player_two", password: "secret-123")
        #expect(user.id == "u-2")
        #expect(await vault.accessToken == "a-2")
        let request = try #require(client.script.recorded("Register").first?.request as? Axon_Auth_V1_RegisterRequest)
        #expect(request.email == "u-2@axon.test")
        #expect(request.username == "player_two")
        #expect(request.password == "secret-123")
        #expect(!request.hasDisplayName)
    }

    @Test
    func wrongCredentialsDoNotTriggerARefresh() async {
        client.script.on("Login") { (_: String?) -> Result<Axon_Auth_V1_LoginResponse, ConnectError> in
            .failure(Fixtures.denied(.unauthenticated, "invalid email or password"))
        }
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-9")))
        let vault = Fixtures.vault(nil, refresher: refresher)
        await #expect(throws: APIError(.unauthenticated, "invalid email or password")) {
            try await api(vault).login(email: "u-1@axon.test", password: "nope")
        }
        #expect(refresher.calls.isEmpty)
        #expect(await vault.hasSession == false)
    }

    @Test
    func loginWithoutTokensFails() async {
        serveLogin(Fixtures.user("u-1"), pair: TokenPair())
        let vault = Fixtures.vault(nil)
        await #expect(throws: APIError.self) {
            try await api(vault).login(email: "u-1@axon.test", password: "secret-123")
        }
        #expect(await vault.hasSession == false)
    }

    @Test
    func restoreWithoutASessionSkipsTheNetwork() async {
        #expect(await api(Fixtures.vault(nil)).restore() == nil)
        #expect(client.script.recorded.isEmpty)
    }

    @Test
    func restoreLoadsTheCurrentUser() async {
        serveMe(acceptedToken: "a-1", user: Fixtures.user("u-1"))
        let user = await api(Fixtures.vault(Fixtures.tokens("a-1"))).restore()
        #expect(user?.id == "u-1")
        #expect(client.script.recorded("GetMe").map(\.authorization) == ["Bearer a-1"])
    }

    @Test
    func restoreRenewsAnExpiredToken() async {
        serveMe(acceptedToken: "a-2", user: Fixtures.user("u-1"))
        let refresher = CountingRefresher(.rotated(Fixtures.tokens("a-2")))
        let user = await api(Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)).restore()
        #expect(user?.id == "u-1")
        #expect(refresher.calls.count == 1)
    }

    @Test
    func restoreReturnsNilWhenTheRefreshIsRejected() async {
        serveMe(acceptedToken: "a-2", user: Fixtures.user("u-1"))
        let refresher = CountingRefresher(.rejected)
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: refresher)
        #expect(await api(vault).restore() == nil)
        #expect(await vault.hasSession == false)
    }

    @Test
    func logoutSendsTheRefreshTokenAndClearsTheVault() async throws {
        client.script.on("Logout") { (_: String?) -> Result<Axon_Auth_V1_LogoutResponse, ConnectError> in
            .success(Axon_Auth_V1_LogoutResponse())
        }
        let vault = Fixtures.vault(Fixtures.tokens("a-1", refresh: "r-1"))
        await api(vault).logout()
        #expect(await vault.hasSession == false)
        let call = try #require(client.script.recorded("Logout").first)
        #expect((call.request as? Axon_Auth_V1_LogoutRequest)?.refreshToken == "r-1")
        #expect(call.authorization == nil)
    }

    @Test
    func logoutClearsTheVaultEvenWhenTheCallFails() async {
        client.script.on("Logout") { (_: String?) -> Result<Axon_Auth_V1_LogoutResponse, ConnectError> in
            .failure(Fixtures.denied(.unavailable))
        }
        let vault = Fixtures.vault(Fixtures.tokens("a-1"))
        await api(vault).logout()
        #expect(await vault.hasSession == false)
        #expect(client.script.recorded("Logout").count == 1)
    }

    @Test
    func profilesAreFetchedOnceAndKeyedByID() async throws {
        typealias Profiles = Result<Axon_Auth_V1_GetUsersPublicProfilesResponse, ConnectError>
        client.script.on("GetUsersPublicProfiles") { (_: String?) -> Profiles in
            var response = Axon_Auth_V1_GetUsersPublicProfilesResponse()
            response.users = ["u-1", "u-2"].map { id in
                var profile = PublicProfile()
                profile.id = id
                profile.username = "name_\(id)"
                return profile
            }
            return .success(response)
        }
        let api = api(Fixtures.vault(Fixtures.tokens("a-1")))
        let profiles = try await api.profiles(ids: ["u-1", "u-2", "u-1", "", "u-3"])
        #expect(profiles.keys.sorted() == ["u-1", "u-2"])
        #expect(profiles["u-2"]?.username == "name_u-2")
        let call = try #require(client.script.recorded("GetUsersPublicProfiles").first)
        #expect(call.authorization == "Bearer a-1")
        #expect((call.request as? Axon_Auth_V1_GetUsersPublicProfilesRequest)?.userIds == ["u-1", "u-2", "u-3"])
    }

    @Test
    func noProfilesNoCall() async throws {
        #expect(try await api(Fixtures.vault(Fixtures.tokens("a-1"))).profiles(ids: []).isEmpty)
        #expect(client.script.recorded.isEmpty)
    }

    @Test
    func signedOutFollowsTheVault() async {
        serveMe(acceptedToken: "a-2", user: Fixtures.user("u-1"))
        let vault = Fixtures.vault(Fixtures.tokens("a-1"), refresher: CountingRefresher(.rejected))
        let api = api(vault)
        _ = await api.restore()
        var signOuts = api.signedOut.makeAsyncIterator()
        #expect(await signOuts.next() != nil)
    }
}
