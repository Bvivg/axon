#if os(iOS)
import AVFoundation
import CallKit
import Foundation

@MainActor
final class CallKitSystem: CallSystem {
    var actions: CallSystemActions?
    private let provider: CXProvider
    private let controller = CXCallController()
    private let relay = CallKitRelay()
    private var calls: [String: UUID] = [:]
    private var videos: [String: Bool] = [:]
    private var endedBySystem: Set<UUID> = []
    private var fallback: Set<String> = []

    init() {
        let configuration = CXProviderConfiguration()
        configuration.supportsVideo = true
        configuration.maximumCallGroups = 1
        configuration.maximumCallsPerCallGroup = 1
        configuration.supportedHandleTypes = [.generic]
        configuration.includesCallsInRecents = true
        provider = CXProvider(configuration: configuration)
        relay.owner = self
        provider.setDelegate(relay, queue: nil)
    }

    func incoming(_ call: String, from caller: String, video: Bool) {
        let id = uuid(for: call)
        videos[call] = video
        provider.reportNewIncomingCall(with: id, update: update(caller, video: video)) { [weak self] error in
            guard error != nil else { return }
            Task { @MainActor in self?.unavailable(call) }
        }
    }

    func outgoing(_ call: String, to callee: String, video: Bool) {
        let id = uuid(for: call)
        videos[call] = video
        let action = CXStartCallAction(call: id, handle: CXHandle(type: .generic, value: callee))
        action.isVideo = video
        action.contactIdentifier = callee
        controller.request(CXTransaction(action: action)) { [weak self] error in
            guard error != nil else { return }
            Task { @MainActor in self?.unavailable(call) }
        }
    }

    func answer(_ call: String) {
        guard let id = calls[call] else { return }
        controller.request(CXTransaction(action: CXAnswerCallAction(call: id))) { _ in }
    }

    func end(_ call: String) {
        guard let id = calls[call] else { return }
        controller.request(CXTransaction(action: CXEndCallAction(call: id))) { _ in }
    }

    func connecting(_ call: String) {
        guard let id = calls[call] else { return }
        provider.reportOutgoingCall(with: id, startedConnectingAt: .now)
    }

    func connected(_ call: String) {
        guard let id = calls[call] else { return }
        provider.reportOutgoingCall(with: id, connectedAt: .now)
    }

    func ended(_ call: String, reason: String) {
        videos[call] = nil
        if fallback.remove(call) != nil {
            actions?.audio(false)
            Task.detached {
                try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
            }
        }
        guard let id = calls.removeValue(forKey: call) else { return }
        if endedBySystem.remove(id) != nil { return }
        provider.reportCall(with: id, endedAt: .now, reason: Self.reason(reason))
    }

    func muted(_ call: String, _ muted: Bool) {
        guard let id = calls[call] else { return }
        controller.request(CXTransaction(action: CXSetMutedCallAction(call: id, muted: muted))) { _ in }
    }

    private func uuid(for call: String) -> UUID {
        if let id = calls[call] { return id }
        let id = UUID(uuidString: call) ?? UUID()
        calls[call] = id
        return id
    }

    private func call(for id: UUID) -> String? {
        calls.first { $0.value == id }?.key
    }

    private func update(_ name: String, video: Bool) -> CXCallUpdate {
        let update = CXCallUpdate()
        update.remoteHandle = CXHandle(type: .generic, value: name)
        update.localizedCallerName = name
        update.hasVideo = video
        update.supportsHolding = false
        update.supportsGrouping = false
        update.supportsUngrouping = false
        update.supportsDTMF = false
        return update
    }

    private func unavailable(_ call: String) {
        calls[call] = nil
        guard fallback.insert(call).inserted else { return }
        let video = videos[call] ?? false
        Task.detached { [weak self] in
            LiveCallMediaFactory.configureAudioSession(video: video)
            try? AVAudioSession.sharedInstance().setActive(true)
            await self?.fallbackActivated(call)
        }
    }

    private func fallbackActivated(_ call: String) {
        guard fallback.contains(call) else { return }
        actions?.audio(true)
    }

    fileprivate func answered(_ id: UUID) {
        guard let call = call(for: id) else { return }
        LiveCallMediaFactory.configureAudioSession(video: videos[call] ?? false)
        actions?.answer(call)
    }

    fileprivate func started(_ id: UUID) {
        guard let call = call(for: id) else { return }
        LiveCallMediaFactory.configureAudioSession(video: videos[call] ?? false)
        provider.reportOutgoingCall(with: id, startedConnectingAt: nil)
    }

    fileprivate func endedBySystem(_ id: UUID) {
        guard let call = call(for: id) else { return }
        endedBySystem.insert(id)
        actions?.end(call)
    }

    fileprivate func mutedBySystem(_ id: UUID, _ muted: Bool) {
        guard let call = call(for: id) else { return }
        actions?.mute(call, muted)
    }

    fileprivate func audio(_ active: Bool) {
        actions?.audio(active)
    }

    fileprivate func reset() {
        for call in calls.keys { actions?.end(call) }
        calls = [:]
        actions?.audio(false)
    }

    static func reason(_ reason: String) -> CXCallEndedReason {
        switch reason {
        case "missed": .unanswered
        case "answered_elsewhere": .answeredElsewhere
        case "declined": .declinedElsewhere
        case "ended", "cancelled", "participants_left", "removed": .remoteEnded
        default: .failed
        }
    }
}

private final class CallKitRelay: NSObject, CXProviderDelegate, @unchecked Sendable {
    nonisolated(unsafe) weak var owner: CallKitSystem?

    func providerDidReset(_ provider: CXProvider) {
        MainActor.assumeIsolated { owner?.reset() }
    }

    func provider(_ provider: CXProvider, perform action: CXAnswerCallAction) {
        let id = action.callUUID
        MainActor.assumeIsolated { owner?.answered(id) }
        action.fulfill()
    }

    func provider(_ provider: CXProvider, perform action: CXStartCallAction) {
        let id = action.callUUID
        MainActor.assumeIsolated { owner?.started(id) }
        action.fulfill()
    }

    func provider(_ provider: CXProvider, perform action: CXEndCallAction) {
        let id = action.callUUID
        MainActor.assumeIsolated { owner?.endedBySystem(id) }
        action.fulfill()
    }

    func provider(_ provider: CXProvider, perform action: CXSetMutedCallAction) {
        let id = action.callUUID
        let muted = action.isMuted
        MainActor.assumeIsolated { owner?.mutedBySystem(id, muted) }
        action.fulfill()
    }

    func provider(_ provider: CXProvider, didActivate audioSession: AVAudioSession) {
        MainActor.assumeIsolated { owner?.audio(true) }
    }

    func provider(_ provider: CXProvider, didDeactivate audioSession: AVAudioSession) {
        MainActor.assumeIsolated { owner?.audio(false) }
    }
}
#endif
