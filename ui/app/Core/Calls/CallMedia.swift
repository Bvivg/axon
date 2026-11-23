import Foundation

struct CallTile: Identifiable, Equatable {
    let id: String
    let userID: String
    let screen: Bool
    let track: AnyObject?

    static func == (lhs: CallTile, rhs: CallTile) -> Bool {
        lhs.id == rhs.id && lhs.userID == rhs.userID && lhs.screen == rhs.screen
            && lhs.track.map(ObjectIdentifier.init) == rhs.track.map(ObjectIdentifier.init)
    }
}

struct CallMediaEvents {
    var changed: @MainActor () -> Void
    var connected: @MainActor () -> Void
    var reconnecting: @MainActor () -> Void
    var failed: @MainActor (String) -> Void
    var state: @MainActor (MediaLinkState) -> Void = { _ in }
}

enum MediaLinkState: Equatable, Sendable {
    case connecting
    case connected
    case disconnected
    case failed
}

@MainActor
protocol CallMedia: AnyObject {
    var call: Call { get }
    var muted: Bool { get }
    var cameraOff: Bool { get }
    var tiles: [CallTile] { get }
    var preview: AnyObject? { get }
    func receive(_ signal: PeerSignal) async
    func gate(_ active: Bool)
    func setMuted(_ muted: Bool)
    func toggleCamera() async
    func switchCamera()
    func close() async
}

struct CallMediaSetup {
    let call: Call
    let me: String
    let video: Bool
    let iceServers: [IceServer]
    let events: CallMediaEvents
}

@MainActor
protocol CallMediaFactory: AnyObject {
    func peer(_ setup: CallMediaSetup, signal: @escaping @MainActor (PeerSignal) -> Void) throws -> any CallMedia
    func sfu(_ setup: CallMediaSetup, url: URL, token: String) async throws -> any CallMedia

    func audio(active: Bool)
}

@MainActor
protocol CapturePermission: AnyObject {
    func request(video: Bool) async -> Bool
}

struct CallSystemActions {
    var answer: @MainActor (String) -> Void
    var end: @MainActor (String) -> Void
    var mute: @MainActor (String, Bool) -> Void
    var audio: @MainActor (Bool) -> Void
}

@MainActor
protocol CallSystem: AnyObject {
    var actions: CallSystemActions? { get set }
    func incoming(_ call: String, from caller: String, video: Bool)
    func outgoing(_ call: String, to callee: String, video: Bool)
    func answer(_ call: String)
    func end(_ call: String)
    func connecting(_ call: String)
    func connected(_ call: String)
    func ended(_ call: String, reason: String)
    func muted(_ call: String, _ muted: Bool)
}
