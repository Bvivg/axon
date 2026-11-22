import AVFoundation
import Foundation
@preconcurrency import WebRTC

@MainActor
final class PeerMedia: CallMedia {
    let call: Call
    private(set) var muted = false
    private(set) var cameraOff = true
    private(set) var tiles: [CallTile] = []
    var preview: AnyObject? { cameraOff ? nil : videoTrack }

    private let me: String
    private let peer: String
    private let factory: RTCPeerConnectionFactory
    private let connection: RTCPeerConnection
    private let relay: PeerRelay
    private let signal: @MainActor (PeerSignal) -> Void
    private let events: CallMediaEvents
    private let audioTrack: RTCAudioTrack
    private var videoTrack: RTCVideoTrack?
    private var videoSource: RTCVideoSource?
    private var capturer: RTCCameraVideoCapturer?
    private var position: AVCaptureDevice.Position = .front
    private var pending: [RTCIceCandidate] = []
    private var makingOffer = false
    private var ignoreOffer = false
    private var settingAnswer = false
    private var closed = false
    private var active = false
    private var queue: Task<Void, Never>?

    init(_ setup: CallMediaSetup, factory: RTCPeerConnectionFactory, signal: @escaping @MainActor (PeerSignal) -> Void) throws {
        let call = setup.call
        let me = setup.me
        self.call = call
        self.me = me
        self.factory = factory
        self.signal = signal
        events = setup.events
        peer = call.others(than: me).first?.userID ?? (call.callerID == me ? "" : call.callerID)
        let configuration = RTCConfiguration()
        configuration.iceServers = setup.iceServers.map {
            RTCIceServer(urlStrings: $0.urls, username: $0.username, credential: $0.credential)
        }
        configuration.sdpSemantics = .unifiedPlan
        configuration.bundlePolicy = .maxBundle
        configuration.rtcpMuxPolicy = .require
        configuration.continualGatheringPolicy = .gatherContinually
        configuration.enableImplicitRollback = true
        let relay = PeerRelay()
        let constraints = RTCMediaConstraints(mandatoryConstraints: nil, optionalConstraints: nil)
        guard let connection = factory.peerConnection(with: configuration, constraints: constraints, delegate: relay) else {
            throw CallFailure.unavailable
        }
        self.relay = relay
        self.connection = connection
        audioTrack = factory.audioTrack(with: factory.audioSource(with: constraints), trackId: "audio-\(UUID().uuidString)")
        audioTrack.isEnabled = false
        connection.add(audioTrack, streamIds: ["local"])
        relay.owner = self
        if setup.video, startCamera() {
            if let videoTrack { connection.add(videoTrack, streamIds: ["local"]) }
        } else if call.mode == .video, call.callerID == me {
            let receiveOnly = RTCRtpTransceiverInit()
            receiveOnly.direction = .recvOnly
            connection.addTransceiver(of: .video, init: receiveOnly)
        }
        if call.callerID == me {
            Task { [weak self] in await self?.offer() }
        }
    }

    func receive(_ signal: PeerSignal) async {
        let previous = queue
        let task = Task { [weak self] in
            await previous?.value
            await self?.apply(signal)
        }
        queue = task
        await task.value
    }

    func gate(_ active: Bool) {
        self.active = active
        audioTrack.isEnabled = active && !muted
        videoTrack?.isEnabled = active && !cameraOff
    }

    func setMuted(_ muted: Bool) {
        self.muted = muted
        gate(active)
        events.changed()
    }

    func toggleCamera() async {
        if cameraOff {
            if videoTrack == nil {
                guard startCamera() else { return }
            } else if let videoSource {
                capturer = RTCCameraVideoCapturer(delegate: videoSource)
                guard startCapture() else { return }
            }
            route(videoTrack)
            cameraOff = false
        } else {
            await capturer?.stopCapture()
            route(nil)
            cameraOff = true
        }
        gate(active)
        events.changed()
    }

    func switchCamera() {
        guard !cameraOff, capturer != nil else { return }
        position = position == .front ? .back : .front
        capturer?.stopCapture()
        if !startCapture() {
            position = position == .front ? .back : .front
            _ = startCapture()
        }
    }

    func close() async {
        guard !closed else { return }
        closed = true
        relay.owner = nil
        await capturer?.stopCapture()
        connection.close()
        tiles = []
        pending = []
    }

    private func offer() async {
        guard !makingOffer, !closed, connection.signalingState == .stable else { return }
        makingOffer = true
        defer { makingOffer = false }
        do {
            try await setLocal()
            guard !closed, let sdp = connection.localDescription?.sdp else { return }
            send(.offer, sdp: sdp)
        } catch {
            if !closed { events.failed("Could not negotiate the call.") }
        }
    }

    private func apply(_ signal: PeerSignal) async {
        guard !closed, signal.generation == call.generation, signal.negotiationID == call.negotiationID else { return }
        do {
            switch signal.kind {
            case .ice:
                try await candidate(signal)
            case .offer, .answer:
                try await describe(signal)
            default:
                return
            }
        } catch {
            if !closed { events.failed("Could not establish the call. Try again.") }
        }
    }

    private func candidate(_ signal: PeerSignal) async throws {
        guard signal.hasIce else { return }
        let ice = signal.ice
        let candidate = RTCIceCandidate(
            sdp: ice.candidate,
            sdpMLineIndex: ice.hasSdpMLineIndex ? ice.sdpMLineIndex : 0,
            sdpMid: ice.hasSdpMid ? ice.sdpMid : nil
        )
        guard connection.remoteDescription != nil else {
            guard pending.count < 128 else { throw CallFailure.unavailable }
            pending.append(candidate)
            return
        }
        do {
            try await add(candidate)
        } catch {
            if !ignoreOffer { throw error }
        }
    }

    private func describe(_ signal: PeerSignal) async throws {
        let offer = signal.kind == .offer
        let collision = offer && (makingOffer || (connection.signalingState != .stable && !settingAnswer))
        ignoreOffer = me <= peer && collision
        if ignoreOffer { return }
        settingAnswer = !offer
        defer { settingAnswer = false }
        try await setRemote(RTCSessionDescription(type: offer ? .offer : .answer, sdp: signal.sdp))
        settingAnswer = false
        let queued = pending
        pending = []
        for candidate in queued { try await add(candidate) }
        guard offer else { return }
        try await setLocal()
        if !closed, let sdp = connection.localDescription?.sdp { send(.answer, sdp: sdp) }
    }

    private func send(_ kind: Axon_Calls_V1_PeerSignalKind, sdp: String? = nil, ice: Axon_Calls_V1_IceCandidate? = nil) {
        guard !closed, let target = call.others(than: me).first, target.hasEndpointID else { return }
        var message = PeerSignal()
        message.callID = call.id
        message.generation = call.generation
        message.negotiationID = call.negotiationID
        message.targetEndpointID = target.endpointID
        message.kind = kind
        if let sdp { message.sdp = sdp }
        if let ice { message.ice = ice }
        signal(message)
    }

    fileprivate func generated(_ sdp: String, mid: String?, index: Int32) {
        var ice = Axon_Calls_V1_IceCandidate()
        ice.candidate = sdp
        if let mid { ice.sdpMid = mid }
        ice.sdpMLineIndex = index
        send(.ice, ice: ice)
    }

    fileprivate func negotiate() {
        guard !closed, connection.localDescription != nil || call.callerID == me else { return }
        Task { [weak self] in await self?.offer() }
    }

    fileprivate func changed(_ state: RTCPeerConnectionState) {
        guard !closed else { return }
        switch state {
        case .connected:
            events.state(.connected)
            events.connected()
        case .failed:
            events.state(.failed)
            events.failed("Connection failed. Try calling again.")
        case .disconnected:
            events.state(.disconnected)
            connection.restartIce()
        case .connecting:
            events.state(.connecting)
        default:
            break
        }
        events.changed()
    }

    fileprivate func received(_ track: RTCMediaStreamTrack) {
        guard !closed, let video = track as? RTCVideoTrack else { return }
        tiles = [CallTile(id: video.trackId, userID: peer, screen: false, track: video)]
        events.changed()
    }
}

extension PeerMedia {
    private func startCamera() -> Bool {
        let source = factory.videoSource()
        videoSource = source
        capturer = RTCCameraVideoCapturer(delegate: source)
        guard startCapture() else {
            capturer = nil
            return false
        }
        let track = factory.videoTrack(with: source, trackId: "video-\(UUID().uuidString)")
        track.isEnabled = false
        videoTrack = track
        cameraOff = false
        return true
    }

    private func startCapture() -> Bool {
        let devices = RTCCameraVideoCapturer.captureDevices()
        guard let capturer, let device = devices.first(where: { $0.position == position }) ?? devices.first else { return false }
        let formats = RTCCameraVideoCapturer.supportedFormats(for: device)
        let format = formats.min { lhs, rhs in
            abs(CMVideoFormatDescriptionGetDimensions(lhs.formatDescription).width - 1280)
                < abs(CMVideoFormatDescriptionGetDimensions(rhs.formatDescription).width - 1280)
        }
        guard let format else { return false }
        let fps = min(30, Int(format.videoSupportedFrameRateRanges.map(\.maxFrameRate).max() ?? 30))
        capturer.startCapture(with: device, format: format, fps: fps)
        return true
    }

    private func route(_ track: RTCVideoTrack?) {
        if let transceiver = connection.transceivers.first(where: { $0.mediaType == .video }) {
            transceiver.sender.track = track
            transceiver.setDirection(track == nil ? .recvOnly : .sendRecv, error: nil)
        } else if let track {
            connection.add(track, streamIds: ["local"])
        }
    }

    private func setLocal() async throws {
        let connection = connection
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
            connection.setLocalDescriptionWithCompletionHandler { error in
                if let error { continuation.resume(throwing: error) } else { continuation.resume() }
            }
        }
    }

    private func setRemote(_ description: RTCSessionDescription) async throws {
        let connection = connection
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
            connection.setRemoteDescription(description) { error in
                if let error { continuation.resume(throwing: error) } else { continuation.resume() }
            }
        }
    }

    private func add(_ candidate: RTCIceCandidate) async throws {
        let connection = connection
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, any Error>) in
            connection.add(candidate) { error in
                if let error { continuation.resume(throwing: error) } else { continuation.resume() }
            }
        }
    }
}

private final class PeerRelay: NSObject, RTCPeerConnectionDelegate, @unchecked Sendable {
    nonisolated(unsafe) weak var owner: PeerMedia?

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange stateChanged: RTCSignalingState) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didAdd stream: RTCMediaStream) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove stream: RTCMediaStream) {}

    func peerConnectionShouldNegotiate(_ peerConnection: RTCPeerConnection) {
        let owner = owner
        Task { @MainActor in owner?.negotiate() }
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceConnectionState) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCIceGatheringState) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didGenerate candidate: RTCIceCandidate) {
        let owner = owner
        let sdp = candidate.sdp
        let mid = candidate.sdpMid
        let index = candidate.sdpMLineIndex
        Task { @MainActor in owner?.generated(sdp, mid: mid, index: index) }
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didRemove candidates: [RTCIceCandidate]) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didOpen dataChannel: RTCDataChannel) {}

    func peerConnection(_ peerConnection: RTCPeerConnection, didChange newState: RTCPeerConnectionState) {
        let owner = owner
        Task { @MainActor in owner?.changed(newState) }
    }

    func peerConnection(_ peerConnection: RTCPeerConnection, didStartReceivingOn transceiver: RTCRtpTransceiver) {
        let owner = owner
        let track = transceiver.receiver.track
        Task { @MainActor in
            if let track { owner?.received(track) }
        }
    }
}
