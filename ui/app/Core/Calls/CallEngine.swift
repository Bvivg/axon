import Foundation
import Observation

struct CallTerminationRequest: Equatable {
    let callID: String
    let action: CallTermination
    let requestID: String
}

enum CallFailure: Error {
    case unavailable
    case credentials
}

@MainActor
@Observable
final class CallEngine {
    var call: Call?
    var media: (any CallMedia)?
    var available = false
    var capabilities = CallCapabilities()
    var pending = false
    var ending = false
    var disconnected = false
    var reconnecting = false
    var error: String?
    var canRetry = false
    var minimized = false
    var revision = 0
    let me: String

    @ObservationIgnored let api: any CallsAPI
    @ObservationIgnored let factory: any CallMediaFactory
    @ObservationIgnored let permission: any CapturePermission
    @ObservationIgnored let system: (any CallSystem)?
    @ObservationIgnored let sfuURL: (String) -> URL?
    @ObservationIgnored var send: (ChatCommand) async -> Bool = { _ in false }
    @ObservationIgnored var names: (String) -> String = { _ in "Axon" }
    @ObservationIgnored var historyChanged: () -> Void = {}
    @ObservationIgnored var profiles: ([String]) async -> Void = { _ in }
    @ObservationIgnored var newRequestID: () -> String = { UUID().uuidString.lowercased() }
    @ObservationIgnored var heartbeatInterval: Duration = .seconds(10)
    @ObservationIgnored var disconnectTimeout: Duration = .seconds(15)
    @ObservationIgnored var endpoint: EndpointRef?
    @ObservationIgnored var socketOpen = false
    @ObservationIgnored var needsResume = false
    @ObservationIgnored var heartbeat: Task<Void, Never>?
    @ObservationIgnored var disconnectTimer: Task<Void, Never>?
    @ObservationIgnored var inbox: Task<Void, Never>?
    @ObservationIgnored var outbox: Task<Void, Never>?
    @ObservationIgnored var versions: [String: Int64] = [:]
    @ObservationIgnored var backlog: [PeerSignal] = []
    @ObservationIgnored var termination: CallTerminationRequest?
    @ObservationIgnored var terminating = false
    @ObservationIgnored var cleaned: Set<String> = []
    @ObservationIgnored var reporting: Set<String> = []
    @ObservationIgnored var ticket = 0
    @ObservationIgnored var connectingKey: String?
    @ObservationIgnored var failedGeneration: String?
    @ObservationIgnored var captureVideo = false
    @ObservationIgnored var announced: Set<String> = []
    @ObservationIgnored var announcedConnecting: Set<String> = []
    @ObservationIgnored var announcedActive: Set<String> = []
    @ObservationIgnored var batchAccepted = true
    @ObservationIgnored var retryAction: (() async -> Void)?

    init(
        api: any CallsAPI,
        me: String,
        factory: any CallMediaFactory,
        permission: any CapturePermission,
        system: (any CallSystem)?,
        sfuURL: @escaping (String) -> URL?
    ) {
        self.api = api
        self.me = me
        self.factory = factory
        self.permission = permission
        self.system = system
        self.sfuURL = sfuURL
        system?.actions = CallSystemActions(
            answer: { [weak self] id in self?.systemAnswered(id) },
            end: { [weak self] id in self?.systemEnded(id) },
            mute: { [weak self] id, muted in self?.systemMuted(id, muted) },
            audio: { [weak factory] active in factory?.audio(active: active) }
        )
    }

    var isIncoming: Bool { call?.isIncoming(for: me) == true }

    var isOngoing: Bool { call?.isOpen == true }

    func activate() async {
        do {
            endpoint = try await register(resume: nil)
            available = true
        } catch {
            available = false
            return
        }
        if let capabilities = try? await api.capabilities() { self.capabilities = capabilities }
        attach()
    }

    func register(resume: EndpointRef?) async throws(APIError) -> EndpointRef {
        do {
            return try await api.register(resume: resume)
        } catch {
            guard resume != nil, [.permissionDenied, .notFound, .failedPrecondition].contains(error.code) else { throw error }
            return try await api.register(resume: nil)
        }
    }

    func socketOpened() {
        socketOpen = true
        guard endpoint != nil else { return }
        guard needsResume else {
            attach()
            return
        }
        needsResume = false
        Task {
            do {
                endpoint = try await register(resume: endpoint)
                attach()
            } catch {
                await fail("Call connection is unavailable.")
            }
        }
    }

    func socketClosed() {
        socketOpen = false
        heartbeat?.cancel()
        heartbeat = nil
        if endpoint != nil { needsResume = true }
        lost()
    }

    func attach() {
        guard socketOpen, let endpoint else { return }
        batchAccepted = true
        var frame = CallSocketFrame()
        frame.endpoint = endpoint
        emit(.attach, frame)
        heartbeat?.cancel()
        let interval = heartbeatInterval
        heartbeat = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: interval)
                guard !Task.isCancelled, let self else { return }
                emit(.heartbeat, CallSocketFrame())
            }
        }
    }

    func resync() {
        batchAccepted = true
        emit(.resync, CallSocketFrame())
    }

    func emit(_ kind: CallFrameKind, _ frame: CallSocketFrame) {
        guard socketOpen else { return }
        let previous = outbox
        let send = send
        outbox = Task {
            await previous?.value
            _ = await send(.call(kind, frame))
        }
    }

    func enqueue(_ event: CallSocketEvent) {
        let previous = inbox
        inbox = Task { [weak self] in
            await previous?.value
            await self?.receive(event)
        }
    }

    func receive(_ event: CallSocketEvent) async {
        if !event.isError, event.endpointEpoch != endpoint?.epoch { return }
        if event.resyncComplete {
            if event.hasMore_p, batchAccepted { resync() }
            return
        }
        let accepted = await process(event)
        guard event.hasEventID, !event.eventID.isEmpty else { return }
        batchAccepted = batchAccepted && accepted
        guard accepted, event.endpointEpoch == endpoint?.epoch else { return }
        var frame = CallSocketFrame()
        frame.eventID = event.eventID
        emit(.ack, frame)
    }

    func process(_ event: CallSocketEvent) async -> Bool {
        if event.isError {
            if event.errorCode == "disconnected" {
                lost()
                return true
            }
            if call?.isOpen != true, !pending { return false }
            await fail("Call connection is unavailable.")
            return false
        }
        if event.hasSignal {
            await route(event.signal)
            return true
        }
        if event.snapshot {
            disconnectTimer?.cancel()
            disconnectTimer = nil
            disconnected = false
            if !event.hasCall, call?.isOpen == true, termination == nil {
                await release()
                call = nil
            }
        }
        if event.hasCall { await apply(event.call, snapshot: event.snapshot) }
        if event.snapshot, let termination { await submit(termination) }
        return true
    }

    func route(_ signal: PeerSignal) async {
        if let media, signal.generation == media.call.generation {
            await media.receive(signal)
        } else if backlog.count < 128 {
            backlog.append(signal)
        } else {
            await fail("Connection messages exceeded the limit.")
        }
        if let call, call.id == signal.callID, signal.generation > call.generation { resync() }
    }

    func lost() {
        guard call?.isOpen == true else { return }
        media?.gate(false)
        disconnected = true
        guard disconnectTimer == nil else { return }
        let timeout = disconnectTimeout
        disconnectTimer = Task { [weak self] in
            try? await Task.sleep(for: timeout)
            guard !Task.isCancelled, let self else { return }
            disconnectTimer = nil
            await fail("Connection lost. Try calling again.")
        }
    }

    func mine(_ call: Call) -> Bool {
        call.belongs(to: endpoint, user: me)
    }

    func mediaAllowed(_ call: Call) -> Bool {
        call.state == .active && !disconnected && (!call.isSfu || call.participant(me)?.mediaReady == true)
    }
}
