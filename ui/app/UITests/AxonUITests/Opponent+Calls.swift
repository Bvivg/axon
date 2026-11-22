import Foundation

extension Opponent {
    func registerEndpoint() async throws -> Axon_Calls_V1_EndpointRef {
        let response: Axon_Calls_V1_RegisterEndpointResponse = try await call(
            "axon.calls.v1.CallsService/RegisterEndpoint", Axon_Calls_V1_RegisterEndpointRequest()
        )
        return response.endpoint
    }

    func startAudioCall(to user: String) async throws -> Axon_Calls_V1_Call {
        var request = Axon_Calls_V1_StartDirectCallRequest()
        callEndpoint = try await registerEndpoint()
        request.endpoint = callEndpoint
        request.requestID = UUID().uuidString.lowercased()
        request.peerUserID = user
        request.mode = .audio
        let response: Axon_Calls_V1_StartDirectCallResponse = try await call(
            "axon.calls.v1.CallsService/StartDirectCall", request
        )
        return response.call
    }

    func acknowledgeCleanup(_ ended: Axon_Calls_V1_Call) async throws {
        var request = Axon_Calls_V1_AcknowledgeCleanupRequest()
        request.endpoint = callEndpoint
        request.requestID = UUID().uuidString.lowercased()
        request.callID = ended.id
        request.generation = ended.generation
        let _: Axon_Calls_V1_AcknowledgeCleanupResponse = try await call(
            "axon.calls.v1.CallsService/AcknowledgeCleanup", request
        )
    }

    func declineCall(_ id: String) async throws -> Axon_Calls_V1_Call {
        var request = Axon_Calls_V1_DeclineCallRequest()
        request.endpoint = try await registerEndpoint()
        request.requestID = UUID().uuidString.lowercased()
        request.callID = id
        let response: Axon_Calls_V1_DeclineCallResponse = try await call(
            "axon.calls.v1.CallsService/DeclineCall", request
        )
        return response.call
    }

    func ringingCall(timeout: Duration = .seconds(20)) async throws -> Axon_Calls_V1_Call {
        let deadline = ContinuousClock.now.advanced(by: timeout)
        while ContinuousClock.now < deadline {
            let response: Axon_Calls_V1_GetActiveCallResponse = try await call(
                "axon.calls.v1.CallsService/GetActiveCall", Axon_Calls_V1_GetActiveCallRequest()
            )
            if response.hasCall, response.call.state == .ringing { return response.call }
            try await Task.sleep(for: .milliseconds(300))
        }
        throw OpponentError.timedOut("a ringing call")
    }

    func endedCall(_ id: String, timeout: Duration = .seconds(20)) async throws -> Axon_Calls_V1_Call {
        let deadline = ContinuousClock.now.advanced(by: timeout)
        while ContinuousClock.now < deadline {
            var request = Axon_Calls_V1_GetCallRequest()
            request.callID = id
            let response: Axon_Calls_V1_GetCallResponse = try await call("axon.calls.v1.CallsService/GetCall", request)
            if response.call.state == .ended { return response.call }
            try await Task.sleep(for: .milliseconds(300))
        }
        throw OpponentError.timedOut("call \(id) to end")
    }
}
