import Foundation

extension CallEngine {
    func connect(_ call: Call) async throws {
        let key = call.mediaKey(for: me)
        guard connectingKey != key else { return }
        connectingKey = key
        defer { if connectingKey == key { connectingKey = nil } }
        ticket += 1
        let ticket = ticket
        let video = media.map { !$0.cameraOff } ?? captureVideo
        let muted = media?.muted ?? false
        if let old = media {
            await old.close()
            guard ticket == self.ticket else { return }
            media = nil
        }
        guard let credentials = try await credentials(for: call), ticket == self.ticket, self.call?.isOpen == true else { return }
        let setup = CallMediaSetup(call: call, me: me, video: video, iceServers: credentials.iceServers, events: mediaEvents(for: call))
        let built = try await build(setup, credentials)
        guard ticket == self.ticket else {
            await built.close()
            return
        }
        built.setMuted(muted)
        media = built
        let queued = backlog
        backlog = queued.filter { $0.generation > call.generation }
        for signal in queued where signal.generation == call.generation { await built.receive(signal) }
        if ticket == self.ticket, let current = self.call { built.gate(mediaAllowed(current)) }
    }

    private func credentials(for call: Call) async throws -> CallCredentials? {
        guard let endpoint else { throw CallFailure.unavailable }
        do {
            return try await api.credentials(for: call, endpoint: endpoint)
        } catch {
            guard error.code == .failedPrecondition, let latest = try? await api.call(call.id) else { throw error }
            guard latest.generation > call.generation else { throw error }
            await apply(latest)
            return nil
        }
    }

    private func build(_ setup: CallMediaSetup, _ credentials: CallCredentials) async throws -> any CallMedia {
        guard setup.call.isSfu else {
            return try factory.peer(setup) { [weak self] signal in self?.signal(signal) }
        }
        guard let path = credentials.url, let token = credentials.token, let url = sfuURL(path) else { throw CallFailure.credentials }
        return try await factory.sfu(setup, url: url, token: token)
    }

    func signal(_ signal: PeerSignal) {
        var frame = CallSocketFrame()
        frame.signal = signal
        emit(.signal, frame)
    }

    func mediaEvents(for call: Call) -> CallMediaEvents {
        CallMediaEvents(
            changed: { [weak self] in self?.revision += 1 },
            connected: { [weak self] in Task { await self?.ready(call) } },
            reconnecting: { [weak self] in Task { await self?.reportReconnecting(call) } },
            failed: { [weak self] message in Task { await self?.mediaFailed(call, message) } },
            state: { [weak self] state in self?.linkChanged(call, state) }
        )
    }

    func ready(_ call: Call) async {
        guard termination?.callID != call.id, endpoint != nil else { return }
        let key = call.mediaKey(for: me)
        if self.call?.mediaKey(for: me) == key { reconnecting = false }
        guard reporting.insert(key).inserted else { return }
        defer { reporting.remove(key) }
        do {
            try await apply(report(call, ConnectionReport(ready: true)))
        } catch {
            if self.call?.mediaKey(for: me) == key { await fail("Could not confirm the connection.") }
        }
    }

    func reportReconnecting(_ call: Call) async {
        guard termination?.callID != call.id, self.call?.mediaKey(for: me) == call.mediaKey(for: me), endpoint != nil else { return }
        reconnecting = true
        do {
            try await apply(report(call, ConnectionReport(ready: false, reconnecting: true)))
        } catch {
            await mediaFailed(call, "Could not reconnect media.")
        }
    }

    func mediaFailed(_ failed: Call, _ message: String) async {
        guard let current = call, current.isOpen, current.mediaKey(for: me) == failed.mediaKey(for: me) else { return }
        if !current.isSfu, !current.everActive, current.hasSfuFallbackAvailable, current.sfuFallbackAvailable {
            failedGeneration = "\(current.id):\(current.generation)"
            await release()
            await reportNotReady(current) { [weak self] in await self?.fail(message) }
            return
        }
        if current.isSfu {
            await release()
            await reportNotReady(current) { [weak self] in self?.resync() }
            error = message
            return
        }
        await fail(message)
    }

    private func reportNotReady(_ call: Call, otherwise: () async -> Void) async {
        do {
            try await apply(report(call, ConnectionReport(ready: false)))
        } catch {
            await otherwise()
        }
    }

    private func report(_ call: Call, _ report: ConnectionReport) async throws -> Call {
        guard let endpoint else { throw CallFailure.unavailable }
        return try await api.report(call, report, mediaVersion: call.mediaVersion(for: me), endpoint: endpoint, request: newRequestID())
    }

    func linkChanged(_ call: Call, _ state: MediaLinkState) {
        guard let current = self.call, current.id == call.id, current.generation == call.generation else { return }
        guard current.everActive, !ending else { return }
        reconnecting = state != .connected
    }

    func toggleMute() {
        guard let media else { return }
        let next = !media.muted
        media.setMuted(next)
        revision += 1
        if let id = call?.id, announced.contains(id) { system?.muted(id, next) }
    }

    func toggleCamera() async {
        await media?.toggleCamera()
        revision += 1
    }

    func switchCamera() {
        media?.switchCamera()
    }

    func invite(_ user: String) async {
        guard let call, !pending, let endpoint else { return }
        pending = true
        defer { pending = false }
        do {
            let updated = try await api.invite(user, to: call, endpoint: endpoint, request: newRequestID())
            if call.isGroup {
                await apply(updated)
            } else {
                await release()
                await cleanup(Self.ended(call))
                await apply(updated, snapshot: true)
            }
        } catch {
            self.error = "Could not add the person to the call."
        }
    }

    func remove(_ user: String) async {
        guard let call, let endpoint else { return }
        do {
            await apply(try await api.remove(user, from: call.id, endpoint: endpoint, request: newRequestID()))
        } catch {
            self.error = "Could not remove the person from the call."
        }
    }

    static func ended(_ call: Call) -> Call {
        var ended = call
        ended.state = .ended
        return ended
    }

    func announce(_ call: Call) {
        guard let system else { return }
        let id = call.id
        if call.state == .ended {
            if announced.remove(id) != nil { system.ended(id, reason: call.endReason) }
            announcedConnecting.remove(id)
            announcedActive.remove(id)
            return
        }
        if call.isIncoming(for: me) {
            if announced.insert(id).inserted { system.incoming(id, from: names(call.callerID), video: call.isVideo) }
            return
        }
        guard announced.contains(id) else { return }
        guard mine(call) || call.callerID == me else {
            announced.remove(id)
            system.ended(id, reason: "answered_elsewhere")
            return
        }
        if call.callerID == me, call.state == .connecting, announcedConnecting.insert(id).inserted { system.connecting(id) }
        if call.state == .active, announcedActive.insert(id).inserted { system.connected(id) }
    }

    func systemAnswered(_ id: String) {
        guard call?.id == id else { return }
        Task { await accept() }
    }

    func systemEnded(_ id: String) {
        guard let call, call.id == id, call.isOpen else { return }
        Task { await hangUp() }
    }

    func systemMuted(_ id: String, _ muted: Bool) {
        guard call?.id == id, let media, media.muted != muted else { return }
        media.setMuted(muted)
        revision += 1
    }
}

extension CallEngine: ChatEventListener {
    func handle(_ event: ChatEvent) {
        switch event {
        case .state(.open):
            socketOpened()
        case .state:
            if socketOpen { socketClosed() }
        case .call(let event):
            enqueue(event)
        default:
            break
        }
    }
}
