@testable import Axon
import Foundation

final class FakeCalls: CallsAPI, @unchecked Sendable {
    var result: Result<[Call], APIError> = .success([])
    var next: String?
    var befores: [String?] = []
    var endpoint = FakeCalls.ref("endpoint-1", epoch: 1)
    var registrations: [EndpointRef?] = []
    var registerErrors: [APIError] = []
    var capabilitiesResult = CallCapabilities(sfu: true, maxParticipants: 6)
    var started: [CallTarget] = []
    var startResult: Result<Call, APIError> = .failure(APIError(.unavailable, "not scripted"))
    var accepted: [String] = []
    var acceptResult: Result<Call, APIError> = .failure(APIError(.unavailable, "not scripted"))
    var terminations: [CallTermination] = []
    var terminateResults: [Result<Call, APIError>] = []
    var credentialsResult: Result<CallCredentials, APIError> = .success(CallCredentials(iceServers: []))
    var reports: [ConnectionReport] = []
    var reportResult: Result<Call, APIError> = .failure(APIError(.unavailable, "not scripted"))
    var cleanups: [String] = []
    var fetched: Result<Call, APIError> = .failure(APIError(.notFound, "not scripted"))
    var invites: [String] = []
    var inviteResult: Result<Call, APIError> = .failure(APIError(.unavailable, "not scripted"))
    var removals: [String] = []

    static func ref(_ id: String, epoch: Int64) -> EndpointRef {
        var ref = EndpointRef()
        ref.id = id
        ref.epoch = epoch
        ref.proof = "proof-\(id)"
        return ref
    }

    func history(before: String?, limit: Int) async throws(APIError) -> CallPage {
        befores.append(before)
        return CallPage(calls: try result.get(), next: next)
    }

    func register(resume: EndpointRef?) async throws(APIError) -> EndpointRef {
        registrations.append(resume)
        if !registerErrors.isEmpty { throw registerErrors.removeFirst() }
        return endpoint
    }

    func capabilities() async throws(APIError) -> CallCapabilities { capabilitiesResult }

    func start(_ target: CallTarget, mode: CallMode, endpoint: EndpointRef, request: String) async throws(APIError) -> Call {
        started.append(target)
        return try startResult.get()
    }

    func accept(_ call: String, endpoint: EndpointRef, request: String) async throws(APIError) -> Call {
        accepted.append(call)
        return try acceptResult.get()
    }

    func terminate(_ call: String, _ action: CallTermination, endpoint: EndpointRef, request: String) async throws(APIError) -> Call {
        terminations.append(action)
        guard !terminateResults.isEmpty else { throw APIError(.unavailable, "not scripted") }
        return try terminateResults.removeFirst().get()
    }

    func credentials(for call: Call, endpoint: EndpointRef) async throws(APIError) -> CallCredentials {
        try credentialsResult.get()
    }

    func report(
        _ call: Call, _ report: ConnectionReport, mediaVersion: Int64?, endpoint: EndpointRef, request: String
    ) async throws(APIError) -> Call {
        reports.append(report)
        return try reportResult.get()
    }

    func cleanup(_ call: Call, mediaVersion: Int64?, endpoint: EndpointRef, request: String) async throws(APIError) {
        cleanups.append(call.id)
    }

    func call(_ id: String) async throws(APIError) -> Call {
        try fetched.get()
    }

    func invite(_ user: String, to call: Call, endpoint: EndpointRef, request: String) async throws(APIError) -> Call {
        invites.append(user)
        return try inviteResult.get()
    }

    func remove(_ user: String, from call: String, endpoint: EndpointRef, request: String) async throws(APIError) -> Call {
        removals.append(user)
        return try inviteResult.get()
    }
}
