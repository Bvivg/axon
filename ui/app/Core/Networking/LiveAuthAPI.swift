import Foundation

struct LiveAuthAPI: AuthAPI {
    let client: any Axon_Auth_V1_AuthServiceClientInterface
    let vault: TokenVault

    private var connect: ConnectClient { ConnectClient(vault: vault) }

    var signedOut: AsyncStream<Void> { vault.signedOut }

    func restore() async -> User? {
        guard await vault.hasSession else { return nil }
        do {
            return try await connect.call { headers in
                await client.getMe(request: Axon_Auth_V1_GetMeRequest(), headers: headers)
            }.user
        } catch {
            return nil
        }
    }

    func register(email: String, username: String, password: String) async throws(APIError) -> User {
        var request = Axon_Auth_V1_RegisterRequest()
        request.email = email
        request.username = username
        request.password = password
        let response = try await client.register(request: request, headers: [:]).unwrapped()
        try await keep(response.tokens)
        return response.user
    }

    func login(email: String, password: String) async throws(APIError) -> User {
        var request = Axon_Auth_V1_LoginRequest()
        request.email = email
        request.password = password
        let response = try await client.login(request: request, headers: [:]).unwrapped()
        try await keep(response.tokens)
        return response.user
    }

    func logout() async {
        let refreshToken = await vault.refreshToken
        await vault.clear()
        guard let refreshToken else { return }
        var request = Axon_Auth_V1_LogoutRequest()
        request.refreshToken = refreshToken
        _ = await client.logout(request: request, headers: [:])
    }

    func profiles(ids: [String]) async throws(APIError) -> [String: PublicProfile] {
        var seen = Set<String>()
        let unique = ids.filter { !$0.isEmpty && seen.insert($0).inserted }
        guard !unique.isEmpty else { return [:] }
        var request = Axon_Auth_V1_GetUsersPublicProfilesRequest()
        request.userIds = unique
        let response = try await connect.call { headers in
            await client.getUsersPublicProfiles(request: request, headers: headers)
        }
        return Dictionary(response.users.map { ($0.id, $0) }) { first, _ in first }
    }

    private func keep(_ pair: TokenPair) async throws(APIError) {
        guard let tokens = AuthTokens(pair) else { throw APIError(.unknown, "the server returned no tokens") }
        await vault.store(tokens)
    }
}
