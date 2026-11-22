import Foundation

extension CallEngine {
    func apply(_ incoming: Call, snapshot: Bool = false) async {
        if call?.id != incoming.id { await profiles(incoming.involved(except: me)) }
        guard fresh(incoming, snapshot: snapshot) else { return }
        historyChanged()
        let current = call
        if incoming.state == .ended, current?.id != incoming.id, !snapshot {
            completeTermination(incoming.id)
            announce(incoming)
            if mine(incoming) { await cleanup(incoming) }
            return
        }
        if let current, current.id != incoming.id, current.isOpen { return }
        if let current, current.id == incoming.id, current.version > incoming.version { return }
        if current?.id != incoming.id { resetSession() }
        call = incoming
        announce(incoming)
        if incoming.state == .ended {
            completeTermination(incoming.id)
            await release()
            if mine(incoming) { await cleanup(incoming) }
            return
        }
        if termination?.callID == incoming.id { return }
        await follow(incoming)
    }

    private func fresh(_ incoming: Call, snapshot: Bool) -> Bool {
        if let version = versions[incoming.id], version > incoming.version || (!snapshot && version == incoming.version) { return false }
        versions[incoming.id] = incoming.version
        if versions.count > 256, let stale = versions.keys.first(where: { $0 != incoming.id }) { versions[stale] = nil }
        return true
    }

    private func resetSession() {
        error = nil
        canRetry = false
        reconnecting = false
        retryAction = nil
        minimized = false
    }

    private func follow(_ incoming: Call) async {
        guard mine(incoming) else {
            if media != nil { await release() }
            return
        }
        if incoming.hasTransportPhase, incoming.transportPhase == "waiting_cleanup" {
            await awaitCleanup(incoming)
            return
        }
        guard incoming.state == .connecting || incoming.state == .active else { return }
        if failedGeneration == "\(incoming.id):\(incoming.generation)" { return }
        if media?.call.mediaKey(for: me) != incoming.mediaKey(for: me) {
            do {
                try await connect(incoming)
            } catch {
                await mediaFailed(incoming, "Could not establish the call. Check microphone access and try again.")
            }
        }
        if let latest = call { media?.gate(mediaAllowed(latest)) }
    }

    private func awaitCleanup(_ incoming: Call) async {
        if incoming.hasPredecessorCallID { return }
        await release()
        guard incoming.hasCleanupGeneration else { return }
        var previous = incoming
        previous.generation = incoming.cleanupGeneration
        await cleanup(previous)
    }

    func start(_ target: CallTarget, mode: CallMode, name: String) async {
        guard available, !pending, call?.isOpen != true else { return }
        call = nil
        let retry: () async -> Void = { [weak self] in await self?.start(target, mode: mode, name: name) }
        await capture(video: mode == .video, retry: retry) { [weak self] in
            guard let self, let endpoint else { throw CallFailure.unavailable }
            let created = try await api.start(target, mode: mode, endpoint: endpoint, request: newRequestID())
            system?.outgoing(created.id, to: name, video: mode == .video)
            announced.insert(created.id)
            await apply(created)
        }
    }

    func accept(audioOnly: Bool = false) async {
        guard let call, !pending, call.isIncoming(for: me) else { return }
        let id = call.id
        if announced.contains(id) { system?.answer(id) }
        let retry: () async -> Void = { [weak self] in await self?.accept(audioOnly: audioOnly) }
        await capture(video: !audioOnly && call.isVideo, retry: retry) { [weak self] in
            guard let self, let endpoint else { throw CallFailure.unavailable }
            let accepted = try await api.accept(id, endpoint: endpoint, request: newRequestID())
            await apply(accepted)
        }
    }

    func capture(video: Bool, retry: @escaping () async -> Void, run: () async throws -> Void) async {
        retryAction = retry
        pending = true
        error = nil
        canRetry = false
        defer { pending = false }
        guard await permission.request(video: video) else {
            error = video
                ? "Allow camera and microphone access in Settings to make video calls."
                : "Allow microphone access in Settings to make calls."
            canRetry = true
            return
        }
        captureVideo = video
        do {
            try await run()
        } catch {
            await release()
            self.error = Self.message(for: error)
            canRetry = true
        }
    }

    func end() async {
        if let call, call.isOpen {
            await terminate(call)
        } else {
            await release()
        }
    }

    func leave() async {
        guard let call else { return }
        await terminate(call, action: .leave)
    }

    func hangUp() async {
        if call?.isGroup == true, let call, mine(call) {
            await leave()
        } else {
            await end()
        }
    }

    func retry() async {
        guard canRetry, let retryAction else { return }
        await retryAction()
    }

    func dismiss() {
        guard let call, !call.isOpen || !mine(call) else { return }
        retryAction = nil
        self.call = nil
        error = nil
        canRetry = false
        minimized = false
    }

    func fail(_ message: String) async {
        error = message
        await end()
    }

    func terminate(_ target: Call, action: CallTermination? = nil) async {
        var call = target
        if let current = self.call, current.id == call.id, current.version >= call.version { call = current }
        guard call.isOpen else { return }
        if let termination, termination.callID == call.id {
            await submit(termination)
            return
        }
        let request = CallTerminationRequest(
            callID: call.id,
            action: action ?? (call.isIncoming(for: me) ? .decline : call.state == .ringing ? .cancel : .end),
            requestID: newRequestID()
        )
        termination = request
        if announced.contains(call.id) { system?.end(call.id) }
        ending = true
        canRetry = false
        await release()
        await submit(request)
    }

    func submit(_ request: CallTerminationRequest) async {
        guard termination == request, !terminating, let endpoint else { return }
        terminating = true
        var endInstead = false
        do {
            let call = try await api.terminate(request.callID, request.action, endpoint: endpoint, request: request.requestID)
            terminating = false
            await apply(call)
        } catch {
            terminating = false
            let raced = request.action == .cancel && [.alreadyExists, .permissionDenied].contains(error.code)
            if raced, let latest = try? await api.call(request.callID) {
                if latest.state == .ended {
                    await apply(latest, snapshot: true)
                } else if latest.state != .ringing, mine(latest), latest.callerID == me {
                    endInstead = true
                }
            }
            if termination == request, !endInstead {
                retryAction = { [weak self] in await self?.submit(request) }
                self.error = Self.cleanupPending
                canRetry = true
            }
        }
        guard endInstead, termination == request else { return }
        let next = CallTerminationRequest(callID: request.callID, action: .end, requestID: newRequestID())
        termination = next
        await submit(next)
    }

    func completeTermination(_ id: String) {
        guard termination?.callID == id else { return }
        termination = nil
        ending = false
        if error == Self.cleanupPending {
            error = nil
            canRetry = false
        }
    }

    func cleanup(_ call: Call) async {
        let version = call.mediaVersion(for: me)
        let key = "\(call.id):\(call.generation):\(endpoint?.epoch ?? 0):\(version ?? 0)"
        guard !cleaned.contains(key), let endpoint else { return }
        do {
            try await api.cleanup(call, mediaVersion: version, endpoint: endpoint, request: newRequestID())
            cleaned.insert(key)
        } catch {
            resync()
        }
    }

    func release() async {
        ticket += 1
        let old = media
        media = nil
        backlog = []
        reconnecting = false
        await old?.close()
    }

    static let cleanupPending = "Call stopped on this device. Waiting for Axon to finish closing it."

    static func message(for error: any Error) -> String {
        guard let error = error as? APIError else { return "Could not start the call." }
        switch error.code {
        case .unavailable: return "The server is unreachable."
        case .resourceExhausted: return "The person is busy on another call."
        case .permissionDenied: return "You can't call this person."
        case .failedPrecondition: return "The call is no longer available."
        default: return "Could not start the call."
        }
    }
}
