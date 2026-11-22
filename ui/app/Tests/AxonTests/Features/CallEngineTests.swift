import Foundation
import Testing
@testable import Axon

enum CallFixtures {
    static let id = "55555555-5555-5555-5555-555555555555"

    static func participant(_ user: String, endpoint: String?, status: String = "joined") -> CallParticipant {
        CallParticipant.with {
            $0.userID = user
            $0.status = status
            if let endpoint {
                $0.endpointID = endpoint
                $0.endpointEpoch = 1
            }
        }
    }

    static func call(
        _ state: CallState,
        caller: String = GameFixtures.rival,
        version: Int64 = 1,
        mode: CallMode = .audio,
        mine: Bool = true,
        reason: String = ""
    ) -> Call {
        Call.with {
            $0.id = id
            $0.roomID = ChatFixtures.room
            $0.callerID = caller
            $0.mode = mode
            $0.state = state
            $0.version = version
            $0.generation = 1
            $0.negotiationID = "n1"
            $0.endReason = reason
            $0.participants = [
                participant(GameFixtures.me, endpoint: mine ? "endpoint-1" : nil),
                participant(GameFixtures.rival, endpoint: "endpoint-r"),
            ]
        }
    }

    static func event(_ call: Call, id: String = "e1", epoch: Int64 = 1) -> CallSocketEvent {
        CallSocketEvent.with {
            $0.eventID = id
            $0.call = call
            $0.endpointEpoch = epoch
        }
    }
}

@MainActor
final class FakeCallMedia: CallMedia {
    let call: Call
    let events: CallMediaEvents
    var muted = false
    var cameraOff = true
    var tiles: [CallTile] = []
    var preview: AnyObject? { nil }
    var received: [PeerSignal] = []
    var gates: [Bool] = []
    var closed = false

    init(call: Call, events: CallMediaEvents) {
        self.call = call
        self.events = events
    }

    func receive(_ signal: PeerSignal) async { received.append(signal) }
    func gate(_ active: Bool) { gates.append(active) }
    func setMuted(_ muted: Bool) { self.muted = muted }
    func toggleCamera() async { cameraOff.toggle() }
    func switchCamera() {}
    func close() async { closed = true }
}

@MainActor
final class FakeCallMediaFactory: CallMediaFactory {
    var peers: [FakeCallMedia] = []
    var sfus: [FakeCallMedia] = []
    var audio: [Bool] = []

    func peer(_ setup: CallMediaSetup, signal: @escaping @MainActor (PeerSignal) -> Void) throws -> any CallMedia {
        let media = FakeCallMedia(call: setup.call, events: setup.events)
        peers.append(media)
        return media
    }

    func sfu(_ setup: CallMediaSetup, url: URL, token: String) async throws -> any CallMedia {
        let media = FakeCallMedia(call: setup.call, events: setup.events)
        sfus.append(media)
        return media
    }

    func audio(active: Bool) { audio.append(active) }
}

@MainActor
final class FakeCapturePermission: CapturePermission {
    var granted = true
    var asked: [Bool] = []

    func request(video: Bool) async -> Bool {
        asked.append(video)
        return granted
    }
}

@MainActor
final class FakeCallSystem: CallSystem {
    var actions: CallSystemActions?
    var log: [String] = []

    func incoming(_ call: String, from caller: String, video: Bool) { log.append("incoming \(caller)") }
    func outgoing(_ call: String, to callee: String, video: Bool) { log.append("outgoing \(callee)") }
    func answer(_ call: String) { log.append("answer") }
    func end(_ call: String) { log.append("end") }
    func connecting(_ call: String) { log.append("connecting") }
    func connected(_ call: String) { log.append("connected") }
    func ended(_ call: String, reason: String) { log.append("ended \(reason)") }
    func muted(_ call: String, _ muted: Bool) { log.append("muted \(muted)") }
}

@MainActor
struct CallEngineTests {
    let api = FakeCalls()
    let factory = FakeCallMediaFactory()
    let permission = FakeCapturePermission()
    let system = FakeCallSystem()
    let sent = Recorder()

    final class Recorder {
        var frames: [(CallFrameKind, CallSocketFrame)] = []
    }

    private func engine() async -> CallEngine {
        let engine = CallEngine(api: api, me: GameFixtures.me, factory: factory, permission: permission, system: system) { path in
            URL(string: "wss://axon.test\(path)")
        }
        engine.heartbeatInterval = .seconds(3600)
        engine.names = { _ in "Grace" }
        let sent = sent
        engine.send = { command in
            if case .call(let kind, let frame) = command { sent.frames.append((kind, frame)) }
            return true
        }
        await engine.activate()
        engine.handle(.state(.open))
        return engine
    }

    private func flush(_ engine: CallEngine) async {
        await engine.outbox?.value
    }

    private func kinds() -> [CallFrameKind] { sent.frames.map(\.0) }

    @Test func registersAnEndpointAndAttachesOnceTheSocketOpens() async {
        let engine = await engine()
        await flush(engine)
        #expect(api.registrations.count == 1)
        #expect(engine.available)
        #expect(engine.capabilities.sfu)
        #expect(kinds() == [.attach])
        #expect(sent.frames.first?.1.endpoint.id == "endpoint-1")
    }

    @Test func announcesAnIncomingCallAndAcknowledgesTheEvent() async {
        let engine = await engine()
        await engine.receive(CallFixtures.event(CallFixtures.call(.ringing, mine: false)))
        await flush(engine)
        #expect(engine.isIncoming)
        #expect(system.log == ["incoming Grace"])
        #expect(kinds() == [.attach, .ack])
        #expect(sent.frames.last?.1.eventID == "e1")
    }

    @Test func ignoresEventsForAnotherEndpointEpoch() async {
        let engine = await engine()
        await engine.receive(CallFixtures.event(CallFixtures.call(.ringing, mine: false), epoch: 2))
        await flush(engine)
        #expect(engine.call == nil)
        #expect(kinds() == [.attach])
    }

    @Test func acceptingConnectsMediaAndReportsItReady() async throws {
        let engine = await engine()
        await engine.receive(CallFixtures.event(CallFixtures.call(.ringing, mine: false)))
        let connecting = CallFixtures.call(.connecting, version: 2)
        api.acceptResult = .success(connecting)
        await engine.accept()
        #expect(api.accepted == [CallFixtures.id])
        #expect(permission.asked == [false])
        #expect(system.log == ["incoming Grace", "answer"])
        let media = try #require(factory.peers.first)
        #expect(media.gates.last == false)
        api.reportResult = .success(CallFixtures.call(.active, version: 3))
        await engine.ready(connecting)
        #expect(api.reports == [ConnectionReport(ready: true)])
        #expect(engine.call?.state == .active)
        #expect(media.gates.last == true)
    }

    @Test func aDeniedMicrophoneStopsTheCallBeforeItStarts() async {
        let engine = await engine()
        permission.granted = false
        await engine.start(.person(GameFixtures.rival), mode: .video, name: "Grace")
        #expect(api.started.isEmpty)
        #expect(permission.asked == [true])
        #expect(engine.error?.contains("camera") == true)
        #expect(engine.canRetry)
        #expect(system.log.isEmpty)
    }

    @Test func anOutgoingCallIsReportedToTheSystemAndConnectsOnceAnswered() async {
        let engine = await engine()
        api.startResult = .success(CallFixtures.call(.ringing, caller: GameFixtures.me))
        await engine.start(.person(GameFixtures.rival), mode: .audio, name: "Grace")
        #expect(api.started == [.person(GameFixtures.rival)])
        #expect(engine.call?.state == .ringing)
        #expect(!engine.isIncoming)
        #expect(factory.peers.isEmpty)
        await engine.receive(CallFixtures.event(CallFixtures.call(.connecting, caller: GameFixtures.me, version: 2)))
        #expect(system.log == ["outgoing Grace", "connecting"])
        #expect(factory.peers.count == 1)
    }

    @Test func decliningEndsTheCallForTheSystemToo() async {
        let engine = await engine()
        await engine.receive(CallFixtures.event(CallFixtures.call(.ringing, mine: false)))
        api.terminateResults = [.success(CallFixtures.call(.ended, version: 2, mine: false, reason: "declined"))]
        await engine.end()
        #expect(api.terminations == [.decline])
        #expect(engine.call?.state == .ended)
        #expect(!engine.ending)
        #expect(system.log == ["incoming Grace", "end", "ended declined"])
    }

    @Test func hangingUpReleasesMediaAndCleansUp() async throws {
        let engine = await engine()
        api.startResult = .success(CallFixtures.call(.connecting, caller: GameFixtures.me))
        await engine.start(.person(GameFixtures.rival), mode: .audio, name: "Grace")
        let media = try #require(factory.peers.first)
        api.terminateResults = [.success(CallFixtures.call(.ended, caller: GameFixtures.me, version: 2, reason: "ended"))]
        await engine.hangUp()
        #expect(api.terminations == [.end])
        #expect(media.closed)
        #expect(engine.media == nil)
        #expect(api.cleanups == [CallFixtures.id])
        engine.dismiss()
        #expect(engine.call == nil)
    }

    @Test func aFailedTerminationCanBeRetried() async {
        let engine = await engine()
        api.startResult = .success(CallFixtures.call(.connecting, caller: GameFixtures.me))
        await engine.start(.person(GameFixtures.rival), mode: .audio, name: "Grace")
        await engine.hangUp()
        #expect(engine.error == CallEngine.cleanupPending)
        #expect(engine.canRetry)
        api.terminateResults = [.success(CallFixtures.call(.ended, caller: GameFixtures.me, version: 2, reason: "ended"))]
        await engine.retry()
        #expect(api.terminations == [.end, .end])
        #expect(engine.error == nil)
        #expect(engine.call?.state == .ended)
    }

    @Test func signalsForMediaNotYetBuiltWaitInTheBacklog() async throws {
        let engine = await engine()
        api.startResult = .success(CallFixtures.call(.ringing, caller: GameFixtures.me))
        await engine.start(.person(GameFixtures.rival), mode: .audio, name: "Grace")
        let offer = PeerSignal.with {
            $0.callID = CallFixtures.id
            $0.generation = 1
            $0.negotiationID = "n1"
            $0.kind = .answer
            $0.sdp = "v=0"
        }
        await engine.receive(CallSocketEvent.with {
            $0.signal = offer
            $0.endpointEpoch = 1
        })
        #expect(engine.backlog.count == 1)
        await engine.receive(CallFixtures.event(CallFixtures.call(.connecting, caller: GameFixtures.me, version: 2), id: "e2"))
        let media = try #require(factory.peers.first)
        #expect(media.received.map(\.sdp) == ["v=0"])
        #expect(engine.backlog.isEmpty)
    }

    @Test func theSystemMuteReachesTheMedia() async throws {
        let engine = await engine()
        api.startResult = .success(CallFixtures.call(.connecting, caller: GameFixtures.me))
        await engine.start(.person(GameFixtures.rival), mode: .audio, name: "Grace")
        let media = try #require(factory.peers.first)
        system.actions?.mute(CallFixtures.id, true)
        #expect(media.muted)
        engine.toggleMute()
        #expect(!media.muted)
        #expect(system.log.last == "muted false")
    }
}
