import Foundation
@preconcurrency import LiveKit

@MainActor
final class SfuMedia: CallMedia {
    let call: Call
    private(set) var muted = false
    private(set) var cameraOff: Bool
    private(set) var tiles: [CallTile] = []
    private(set) var preview: AnyObject?

    private let room: LiveKit.Room
    private let relay: SfuRelay
    private let events: CallMediaEvents
    private var active = false
    private var stopped = false
    private var joined = false
    private var gating: Task<Void, Never>?

    init(call: Call, video: Bool, events: CallMediaEvents) {
        self.call = call
        self.events = events
        cameraOff = !video
        room = LiveKit.Room()
        relay = SfuRelay()
        relay.owner = self
        room.add(delegate: relay)
    }

    func join(url: URL, token: String, iceServers: [IceServer]) async throws {
        let servers = iceServers.map { LiveKit.IceServer(urls: $0.urls, username: $0.username, credential: $0.credential) }
        do {
            let options = ConnectOptions(autoSubscribe: true, iceServers: servers)
            try await room.connect(url: url.absoluteString, token: token, connectOptions: options)
            guard !stopped else { return }
            try await room.localParticipant.setMicrophone(enabled: false)
            if !cameraOff {
                let camera = try? await room.localParticipant.setCamera(enabled: false)
                if camera == nil { cameraOff = true }
            }
            joined = true
            refresh()
            events.connected()
        } catch {
            await close()
            throw error
        }
    }

    func receive(_ signal: PeerSignal) async {}

    func gate(_ active: Bool) {
        self.active = active
        applyGate()
    }

    func setMuted(_ muted: Bool) {
        self.muted = muted
        applyGate()
        events.changed()
    }

    func toggleCamera() async {
        cameraOff.toggle()
        applyGate()
        await gating?.value
        refresh()
    }

    func switchCamera() {
        let capturer = (room.localParticipant.firstCameraVideoTrack as? LocalVideoTrack)?.capturer as? CameraCapturer
        Task { _ = try? await capturer?.switchCameraPosition() }
    }

    func close() async {
        guard !stopped else { return }
        stopped = true
        relay.owner = nil
        gating?.cancel()
        await room.disconnect()
        tiles = []
        preview = nil
    }

    private func applyGate() {
        guard joined, !stopped else { return }
        let microphone = active && !muted
        let camera = active && !cameraOff
        let participant = room.localParticipant
        let previous = gating
        gating = Task {
            await previous?.value
            _ = try? await participant.setMicrophone(enabled: microphone)
            _ = try? await participant.setCamera(enabled: camera)
        }
    }

    fileprivate func refresh() {
        guard !stopped else { return }
        var next: [CallTile] = []
        for participant in room.remoteParticipants.values {
            let user = participant.identity?.stringValue ?? ""
            for publication in participant.videoTracks {
                guard let track = publication.track as? VideoTrack, !publication.isMuted else { continue }
                let screen = publication.source == .screenShareVideo
                next.append(CallTile(id: publication.sid.stringValue, userID: user, screen: screen, track: track))
            }
        }
        tiles = next.sorted { $0.id < $1.id }
        preview = cameraOff ? nil : room.localParticipant.firstCameraVideoTrack
        events.changed()
    }

    fileprivate func connection(_ state: ConnectionState) {
        guard !stopped, joined else { return }
        switch state {
        case .reconnecting:
            events.state(.disconnected)
            events.reconnecting()
        case .connected:
            events.state(.connected)
            events.connected()
        case .disconnected:
            events.state(.failed)
            events.failed("Media connection ended.")
        default:
            break
        }
    }
}

private final class SfuRelay: RoomDelegate, @unchecked Sendable {
    nonisolated(unsafe) weak var owner: SfuMedia?

    func room(_ room: LiveKit.Room, didUpdateConnectionState connectionState: ConnectionState, from oldConnectionState: ConnectionState) {
        let owner = owner
        Task { @MainActor in owner?.connection(connectionState) }
    }

    func room(_ room: LiveKit.Room, participant: RemoteParticipant, didSubscribeTrack publication: RemoteTrackPublication) {
        let owner = owner
        Task { @MainActor in owner?.refresh() }
    }

    func room(_ room: LiveKit.Room, participant: RemoteParticipant, didUnsubscribeTrack publication: RemoteTrackPublication) {
        let owner = owner
        Task { @MainActor in owner?.refresh() }
    }

    func room(_ room: LiveKit.Room, participant: Participant, trackPublication: TrackPublication, didUpdateIsMuted isMuted: Bool) {
        let owner = owner
        Task { @MainActor in owner?.refresh() }
    }

    func room(_ room: LiveKit.Room, participantDidDisconnect participant: RemoteParticipant) {
        let owner = owner
        Task { @MainActor in owner?.refresh() }
    }
}
