import Connect
import Foundation
import SwiftProtobuf
import Synchronization
@testable import Axon

struct RecordedCall: Sendable {
    let method: String
    let authorization: String?
    let request: any SwiftProtobuf.Message
}

final class CallScript: Sendable {
    private let calls = Mutex<[RecordedCall]>([])
    private let handlers = Mutex<[String: any Sendable]>([:])

    var recorded: [RecordedCall] { calls.withLock { $0 } }

    func recorded(_ method: String) -> [RecordedCall] {
        recorded.filter { $0.method == method }
    }

    func on<Output: ProtobufMessage>(
        _ method: String,
        _ handler: @escaping @Sendable (String?) async -> Result<Output, ConnectError>
    ) {
        handlers.withLock { $0[method] = handler }
    }

    func respond<Input: ProtobufMessage, Output: ProtobufMessage>(
        _ method: String,
        _ request: Input,
        _ headers: Headers
    ) async -> ResponseMessage<Output> {
        let authorization = headers["Authorization"]?.first
        calls.withLock { $0.append(RecordedCall(method: method, authorization: authorization, request: request)) }
        let handler = handlers.withLock { $0[method] } as? @Sendable (String?) async -> Result<Output, ConnectError>
        guard let handler else {
            let missing = ConnectError(code: .unimplemented, message: method)
            return ResponseMessage(code: .unimplemented, result: .failure(missing))
        }
        let result = await handler(authorization)
        switch result {
        case .success: return ResponseMessage(result: result)
        case .failure(let error): return ResponseMessage(code: error.code, result: result)
        }
    }
}

final class ScriptedAuthClient: Axon_Auth_V1_AuthServiceClientInterface {
    let script = CallScript()

    func register(
        request: Axon_Auth_V1_RegisterRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_RegisterResponse> {
        await script.respond("Register", request, headers)
    }

    func login(
        request: Axon_Auth_V1_LoginRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_LoginResponse> {
        await script.respond("Login", request, headers)
    }

    func refreshToken(
        request: Axon_Auth_V1_RefreshTokenRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_RefreshTokenResponse> {
        await script.respond("RefreshToken", request, headers)
    }

    func logout(
        request: Axon_Auth_V1_LogoutRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_LogoutResponse> {
        await script.respond("Logout", request, headers)
    }

    func getMe(
        request: Axon_Auth_V1_GetMeRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_GetMeResponse> {
        await script.respond("GetMe", request, headers)
    }

    func startOauth(
        request: Axon_Auth_V1_StartOAuthRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_StartOAuthResponse> {
        await script.respond("StartOAuth", request, headers)
    }

    func completeOauth(
        request: Axon_Auth_V1_CompleteOAuthRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_CompleteOAuthResponse> {
        await script.respond("CompleteOAuth", request, headers)
    }

    func updateProfile(
        request: Axon_Auth_V1_UpdateProfileRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_UpdateProfileResponse> {
        await script.respond("UpdateProfile", request, headers)
    }

    func listSessions(
        request: Axon_Auth_V1_ListSessionsRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_ListSessionsResponse> {
        await script.respond("ListSessions", request, headers)
    }

    func revokeSession(
        request: Axon_Auth_V1_RevokeSessionRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_RevokeSessionResponse> {
        await script.respond("RevokeSession", request, headers)
    }

    func getUserByEmail(
        request: Axon_Auth_V1_GetUserByEmailRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_GetUserByEmailResponse> {
        await script.respond("GetUserByEmail", request, headers)
    }

    func listRoles(
        request: Axon_Auth_V1_ListRolesRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_ListRolesResponse> {
        await script.respond("ListRoles", request, headers)
    }

    func assignRole(
        request: Axon_Auth_V1_AssignRoleRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_AssignRoleResponse> {
        await script.respond("AssignRole", request, headers)
    }

    func revokeRole(
        request: Axon_Auth_V1_RevokeRoleRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_RevokeRoleResponse> {
        await script.respond("RevokeRole", request, headers)
    }

    func searchUsers(
        request: Axon_Auth_V1_SearchUsersRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_SearchUsersResponse> {
        await script.respond("SearchUsers", request, headers)
    }

    func getUsersPublicProfiles(
        request: Axon_Auth_V1_GetUsersPublicProfilesRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_GetUsersPublicProfilesResponse> {
        await script.respond("GetUsersPublicProfiles", request, headers)
    }

    func getPublicProfileByUsername(
        request: Axon_Auth_V1_GetPublicProfileByUsernameRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_GetPublicProfileByUsernameResponse> {
        await script.respond("GetPublicProfileByUsername", request, headers)
    }

    func updateLastSeen(
        request: Axon_Auth_V1_UpdateLastSeenRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Auth_V1_UpdateLastSeenResponse> {
        await script.respond("UpdateLastSeen", request, headers)
    }
}

final class ScriptedGameClient: Axon_Game_V1_GameServiceClientInterface {
    let script = CallScript()

    func createSession(
        request: Axon_Game_V1_CreateSessionRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_CreateSessionResponse> {
        await script.respond("CreateSession", request, headers)
    }

    func joinSession(
        request: Axon_Game_V1_JoinSessionRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_JoinSessionResponse> {
        await script.respond("JoinSession", request, headers)
    }

    func getSession(
        request: Axon_Game_V1_GetSessionRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_GetSessionResponse> {
        await script.respond("GetSession", request, headers)
    }

    func listSessions(
        request: Axon_Game_V1_ListSessionsRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_ListSessionsResponse> {
        await script.respond("ListSessions", request, headers)
    }

    func findMatch(
        request: Axon_Game_V1_FindMatchRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_FindMatchResponse> {
        await script.respond("FindMatch", request, headers)
    }

    func cancelMatch(
        request: Axon_Game_V1_CancelMatchRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_CancelMatchResponse> {
        await script.respond("CancelMatch", request, headers)
    }

    func getMatch(
        request: Axon_Game_V1_GetMatchRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_GetMatchResponse> {
        await script.respond("GetMatch", request, headers)
    }

    func getRatings(
        request: Axon_Game_V1_GetRatingsRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_GetRatingsResponse> {
        await script.respond("GetRatings", request, headers)
    }

    func leaveSession(
        request: Axon_Game_V1_LeaveSessionRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_LeaveSessionResponse> {
        await script.respond("LeaveSession", request, headers)
    }

    func getSessionHistory(
        request: Axon_Game_V1_GetSessionHistoryRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_GetSessionHistoryResponse> {
        await script.respond("GetSessionHistory", request, headers)
    }

    func listLiveSessions(
        request: Axon_Game_V1_ListLiveSessionsRequest,
        headers: Headers
    ) async -> ResponseMessage<Axon_Game_V1_ListLiveSessionsResponse> {
        await script.respond("ListLiveSessions", request, headers)
    }
}
