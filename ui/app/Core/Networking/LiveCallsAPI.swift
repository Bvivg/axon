import Foundation

struct LiveCallsAPI: CallsAPI {
    let client: any Axon_Calls_V1_CallsServiceClientInterface
    let connect: ConnectClient

    func history(before: String?, limit: Int) async throws(APIError) -> CallPage {
        var request = Axon_Calls_V1_ListCallsRequest()
        request.limit = Int32(limit)
        if let before { request.beforeID = before }
        let response = try await connect.call { headers in
            await client.listCalls(request: request, headers: headers)
        }
        let next = response.hasNextBeforeID && !response.nextBeforeID.isEmpty ? response.nextBeforeID : nil
        return CallPage(calls: response.calls, next: next)
    }

    func register(resume: EndpointRef?) async throws(APIError) -> EndpointRef {
        var request = Axon_Calls_V1_RegisterEndpointRequest()
        if let resume { request.resume = resume }
        request.supportsSfu = true
        request.supportsMigration = false
        return try await connect.call { headers in
            await client.registerEndpoint(request: request, headers: headers)
        }.endpoint
    }

    func capabilities() async throws(APIError) -> CallCapabilities {
        let response = try await connect.call { headers in
            await client.getCallCapabilities(request: Axon_Calls_V1_GetCallCapabilitiesRequest(), headers: headers)
        }
        return CallCapabilities(sfu: response.sfuEnabled, maxParticipants: Int(max(response.maxParticipants, 2)))
    }

    func start(_ target: CallTarget, mode: CallMode, endpoint: EndpointRef, request id: String) async throws(APIError) -> Call {
        switch target {
        case .person(let peer):
            var request = Axon_Calls_V1_StartDirectCallRequest()
            request.endpoint = endpoint
            request.requestID = id
            request.peerUserID = peer
            request.mode = mode
            return try await connect.call { headers in await client.startDirectCall(request: request, headers: headers) }.call
        case .group(let room):
            var request = Axon_Calls_V1_StartGroupCallRequest()
            request.endpoint = endpoint
            request.requestID = id
            request.roomID = room
            request.mode = mode
            return try await connect.call { headers in await client.startGroupCall(request: request, headers: headers) }.call
        case .room(let room):
            var request = Axon_Calls_V1_StartCallRequest()
            request.endpoint = endpoint
            request.requestID = id
            request.roomID = room
            request.mode = mode
            return try await connect.call { headers in await client.startCall(request: request, headers: headers) }.call
        }
    }

    func accept(_ call: String, endpoint: EndpointRef, request id: String) async throws(APIError) -> Call {
        var request = Axon_Calls_V1_AcceptCallRequest()
        request.endpoint = endpoint
        request.requestID = id
        request.callID = call
        return try await connect.call { headers in await client.acceptCall(request: request, headers: headers) }.call
    }

    func terminate(_ call: String, _ action: CallTermination, endpoint: EndpointRef, request id: String) async throws(APIError) -> Call {
        switch action {
        case .decline:
            var request = Axon_Calls_V1_DeclineCallRequest()
            request.endpoint = endpoint
            request.requestID = id
            request.callID = call
            return try await connect.call { headers in await client.declineCall(request: request, headers: headers) }.call
        case .cancel:
            var request = Axon_Calls_V1_CancelCallRequest()
            request.endpoint = endpoint
            request.requestID = id
            request.callID = call
            return try await connect.call { headers in await client.cancelCall(request: request, headers: headers) }.call
        case .end:
            var request = Axon_Calls_V1_EndCallRequest()
            request.endpoint = endpoint
            request.requestID = id
            request.callID = call
            return try await connect.call { headers in await client.endCall(request: request, headers: headers) }.call
        case .leave:
            var request = Axon_Calls_V1_LeaveCallRequest()
            request.endpoint = endpoint
            request.requestID = id
            request.callID = call
            return try await connect.call { headers in await client.leaveCall(request: request, headers: headers) }.call
        }
    }

    func credentials(for call: Call, endpoint: EndpointRef) async throws(APIError) -> CallCredentials {
        if call.isSfu {
            var request = Axon_Calls_V1_GetSfuCredentialsRequest()
            request.endpoint = endpoint
            request.callID = call.id
            request.generation = call.generation
            let response = try await connect.call { headers in await client.getSfuCredentials(request: request, headers: headers) }
            return CallCredentials(iceServers: response.iceServers, url: response.url, token: response.token)
        }
        var request = Axon_Calls_V1_GetMediaCredentialsRequest()
        request.endpoint = endpoint
        request.callID = call.id
        request.generation = call.generation
        let response = try await connect.call { headers in await client.getMediaCredentials(request: request, headers: headers) }
        return CallCredentials(iceServers: response.iceServers)
    }

    func report(
        _ call: Call, _ report: ConnectionReport, mediaVersion: Int64?, endpoint: EndpointRef, request id: String
    ) async throws(APIError) -> Call {
        var request = Axon_Calls_V1_ReportConnectionRequest()
        request.endpoint = endpoint
        request.requestID = id
        request.callID = call.id
        request.generation = call.generation
        request.ready = report.ready
        if report.reconnecting { request.reconnecting = true }
        if let mediaVersion { request.mediaVersion = mediaVersion }
        return try await connect.call { headers in await client.reportConnection(request: request, headers: headers) }.call
    }

    func cleanup(_ call: Call, mediaVersion: Int64?, endpoint: EndpointRef, request id: String) async throws(APIError) {
        var request = Axon_Calls_V1_AcknowledgeCleanupRequest()
        request.endpoint = endpoint
        request.requestID = id
        request.callID = call.id
        request.generation = call.generation
        if let mediaVersion { request.mediaVersion = mediaVersion }
        _ = try await connect.call { headers in await client.acknowledgeCleanup(request: request, headers: headers) }
    }

    func call(_ id: String) async throws(APIError) -> Call {
        var request = Axon_Calls_V1_GetCallRequest()
        request.callID = id
        return try await connect.call { headers in await client.getCall(request: request, headers: headers) }.call
    }

    func invite(_ user: String, to call: Call, endpoint: EndpointRef, request id: String) async throws(APIError) -> Call {
        if call.isGroup {
            var request = Axon_Calls_V1_InviteCallParticipantRequest()
            request.endpoint = endpoint
            request.requestID = id
            request.callID = call.id
            request.userID = user
            return try await connect.call { headers in await client.inviteCallParticipant(request: request, headers: headers) }.call
        }
        var request = Axon_Calls_V1_StartGroupCallFromCallRequest()
        request.endpoint = endpoint
        request.requestID = id
        request.callID = call.id
        request.userID = user
        return try await connect.call { headers in await client.startGroupCallFromCall(request: request, headers: headers) }.call
    }

    func remove(_ user: String, from call: String, endpoint: EndpointRef, request id: String) async throws(APIError) -> Call {
        var request = Axon_Calls_V1_RemoveCallParticipantRequest()
        request.endpoint = endpoint
        request.requestID = id
        request.callID = call
        request.userID = user
        return try await connect.call { headers in await client.removeCallParticipant(request: request, headers: headers) }.call
    }
}
