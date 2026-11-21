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

    func startOAuth(_ provider: OAuthProvider, challenge: String) async throws(APIError) -> OAuthStart {
        var request = Axon_Auth_V1_StartOAuthRequest()
        request.provider = provider.proto
        request.appChallenge = challenge
        let response = try await client.startOauth(request: request, headers: [:]).unwrapped()
        return try Self.start(response, provider)
    }

    func completeOAuth(_ provider: OAuthProvider, code: String, state: String, verifier: String) async throws(APIError) -> User {
        var request = Axon_Auth_V1_CompleteOAuthRequest()
        request.provider = provider.proto
        request.code = code
        request.state = state
        request.appVerifier = verifier
        let response = try await client.completeOauth(request: request, headers: [:]).unwrapped()
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

    func account() async throws(APIError) -> Account {
        let response = try await connect.call { headers in
            await client.getMe(request: Axon_Auth_V1_GetMeRequest(), headers: headers)
        }
        return Account(user: response.user, linked: Set(response.linkedProviders.compactMap(OAuthProvider.init(proto:))))
    }

    func updateProfile(_ changes: ProfileChanges) async throws(APIError) -> User {
        var request = Axon_Auth_V1_UpdateProfileRequest()
        if let email = changes.email { request.email = email }
        if let username = changes.username { request.username = username }
        if let firstName = changes.firstName { request.firstName = firstName }
        if let lastName = changes.lastName { request.lastName = lastName }
        if let nickname = changes.nickname { request.nickname = nickname }
        return try await connect.call { headers in
            await client.updateProfile(request: request, headers: headers)
        }.user
    }

    func sessions() async throws(APIError) -> [SignInSession] {
        try await connect.call { headers in
            await client.listSessions(request: Axon_Auth_V1_ListSessionsRequest(), headers: headers)
        }.sessions
    }

    func revokeSession(id: String) async throws(APIError) {
        var request = Axon_Auth_V1_RevokeSessionRequest()
        request.sessionID = id
        _ = try await connect.call { headers in
            await client.revokeSession(request: request, headers: headers)
        }
    }

    func startLink(_ provider: OAuthProvider, challenge: String) async throws(APIError) -> OAuthStart {
        var request = Axon_Auth_V1_StartOAuthRequest()
        request.provider = provider.proto
        request.appChallenge = challenge
        request.link = true
        let response = try await connect.call { headers in
            await client.startOauth(request: request, headers: headers)
        }
        return try Self.start(response, provider)
    }

    func completeLink(_ provider: OAuthProvider, code: String, state: String, verifier: String) async throws(APIError) -> User {
        var request = Axon_Auth_V1_CompleteOAuthRequest()
        request.provider = provider.proto
        request.code = code
        request.state = state
        request.appVerifier = verifier
        let response = try await connect.call { headers in
            await client.completeOauth(request: request, headers: headers)
        }
        guard response.linked else { throw APIError(.unknown, "\(provider.title) was not connected.") }
        return response.user
    }

    private static func start(_ response: Axon_Auth_V1_StartOAuthResponse, _ provider: OAuthProvider) throws(APIError) -> OAuthStart {
        guard let url = URL(string: response.authorizationURL), ["https", "http"].contains(url.scheme ?? ""), !response.state.isEmpty else {
            throw APIError(.unknown, "The server returned no \(provider.title) sign-in page.")
        }
        return OAuthStart(authorizationURL: url, state: response.state)
    }

    private func keep(_ pair: TokenPair) async throws(APIError) {
        guard let tokens = AuthTokens(pair) else { throw APIError(.unknown, "the server returned no tokens") }
        await vault.store(tokens)
    }
}
