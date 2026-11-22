import Foundation

typealias CallSocketFrame = Axon_Calls_V1_CallSocketFrame
typealias CallSocketEvent = Axon_Calls_V1_CallSocketEvent
typealias PeerSignal = Axon_Calls_V1_PeerSignal
typealias EndpointRef = Axon_Calls_V1_EndpointRef
typealias CallMode = Axon_Calls_V1_CallMode
typealias CallState = Axon_Calls_V1_CallState
typealias IceServer = Axon_Calls_V1_IceServer
typealias CallParticipant = Axon_Calls_V1_Participant

enum CallTarget: Hashable, Sendable {
    case person(String)
    case room(String)
    case group(String)
}

enum CallTermination: String, Sendable {
    case decline
    case cancel
    case end
    case leave
}

struct CallCapabilities: Equatable, Sendable {
    var sfu = false
    var maxParticipants = 6
}

struct CallCredentials: Equatable, Sendable {
    var iceServers: [IceServer]
    var url: String?
    var token: String?
}

struct CallPage: Equatable, Sendable {
    var calls: [Call]
    var next: String?
}

struct ConnectionReport: Equatable, Sendable {
    var ready: Bool
    var reconnecting = false
}

extension Call {
    var isOpen: Bool { state != .ended }

    var isSfu: Bool { hasTransport && transport == "sfu" }

    func isIncoming(for me: String) -> Bool {
        guard state != .ended else { return false }
        if isGroup {
            return participants.contains { $0.userID == me && $0.status == "invited" }
        }
        return state == .ringing && callerID != me
    }

    func participant(_ user: String) -> CallParticipant? {
        participants.first { $0.userID == user }
    }

    func belongs(to endpoint: EndpointRef?, user: String) -> Bool {
        guard let endpoint else { return false }
        return participants.contains {
            $0.userID == user && $0.hasEndpointID && $0.endpointID == endpoint.id && $0.endpointEpoch == endpoint.epoch
        }
    }

    func mediaVersion(for user: String) -> Int64? {
        guard let mine = participant(user), mine.hasMediaVersion else { return nil }
        return mine.mediaVersion
    }

    func mediaKey(for user: String) -> String {
        let mine = participant(user)
        return "\(id):\(generation):\(mine?.endpointEpoch ?? 0):\(mine?.mediaVersion ?? 0)"
    }

    func involved(except me: String) -> [String] {
        let users = participants.map(\.userID) + [callerID]
        return Array(Set(users.filter { $0 != me && !$0.isEmpty }))
    }

    func others(than me: String) -> [CallParticipant] {
        participants.filter { $0.userID != me }
    }

    var connectedSince: Date? { hasStartedAt ? startedAt.date : nil }
}

extension CallSocketEvent {
    var isError: Bool { hasErrorCode && !errorCode.isEmpty }
}
