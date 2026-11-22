import AVFoundation
import Foundation
@preconcurrency import LiveKit
@preconcurrency import WebRTC

@MainActor
final class LiveCallMediaFactory: CallMediaFactory {
    private let manualAudio: Bool
    private lazy var connections: RTCPeerConnectionFactory = {
        RTCInitializeSSL()
        return RTCPeerConnectionFactory(encoderFactory: RTCDefaultVideoEncoderFactory(), decoderFactory: RTCDefaultVideoDecoderFactory())
    }()

    init(manualAudio: Bool) {
        self.manualAudio = manualAudio
        #if os(iOS)
        if manualAudio {
            let session = RTCAudioSession.sharedInstance()
            session.useManualAudio = true
            session.isAudioEnabled = false
            AudioManager.shared.audioSession.isAutomaticConfigurationEnabled = false
            try? AudioManager.shared.setEngineAvailability(.none)
        }
        #endif
    }

    func peer(_ setup: CallMediaSetup, signal: @escaping @MainActor (PeerSignal) -> Void) throws -> any CallMedia {
        try PeerMedia(setup, factory: connections, signal: signal)
    }

    func sfu(_ setup: CallMediaSetup, url: URL, token: String) async throws -> any CallMedia {
        let media = SfuMedia(call: setup.call, video: setup.video, events: setup.events)
        try await media.join(url: url, token: token, iceServers: setup.iceServers)
        return media
    }

    func audio(active: Bool) {
        #if os(iOS)
        guard manualAudio else { return }
        let session = AVAudioSession.sharedInstance()
        let rtc = RTCAudioSession.sharedInstance()
        if active {
            rtc.audioSessionDidActivate(session)
        } else {
            rtc.audioSessionDidDeactivate(session)
        }
        rtc.isAudioEnabled = active
        try? AudioManager.shared.setEngineAvailability(active ? .default : .none)
        #endif
    }

    nonisolated static func configureAudioSession(video: Bool) {
        #if os(iOS)
        let session = AVAudioSession.sharedInstance()
        let options: AVAudioSession.CategoryOptions = [.allowBluetoothHFP, .allowBluetoothA2DP]
        try? session.setCategory(.playAndRecord, mode: video ? .videoChat : .voiceChat, options: options)
        #endif
    }
}

@MainActor
final class SystemCapturePermission: CapturePermission {
    func request(video: Bool) async -> Bool {
        guard await AVAudioApplication.requestRecordPermission() else { return false }
        if video, AVCaptureDevice.authorizationStatus(for: .video) == .notDetermined {
            _ = await AVCaptureDevice.requestAccess(for: .video)
        }
        return true
    }
}
