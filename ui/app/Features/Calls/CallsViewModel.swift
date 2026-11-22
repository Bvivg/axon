import Foundation
import Observation

enum CallFilter: String, CaseIterable, Identifiable, Sendable {
    case all = "All"
    case missed = "Missed"
    case incoming = "Incoming"
    case outgoing = "Outgoing"
    case audio = "Audio"
    case video = "Video"

    var id: String { rawValue }
}

@MainActor
@Observable
final class CallsViewModel {
    private(set) var calls: [Call] = []
    private(set) var people: [String: PublicProfile] = [:]
    private(set) var loaded = false
    private(set) var error: String?
    var query = ""
    var filter: CallFilter = .all
    let me: String
    @ObservationIgnored private let services: Services
    @ObservationIgnored private var pendingRefresh: Task<Void, Never>?

    init(services: Services, me: String) {
        self.services = services
        self.me = me
    }

    var visible: [Call] {
        let needle = query.trimmingCharacters(in: .whitespaces)
        return calls.filter { call in
            matches(call) && (needle.isEmpty || name(of: call).localizedStandardContains(needle))
        }
    }

    func name(of call: Call) -> String {
        if call.isGroup { return "Group call" }
        guard let peer = call.peer(for: me) else { return "Call" }
        return people[peer]?.shownName ?? "Direct call"
    }

    func avatar(of call: Call) -> URL? {
        guard !call.isGroup, let peer = call.peer(for: me) else { return nil }
        return people[peer]?.avatarLink
    }

    func summary(of call: Call) -> String {
        let direction = call.direction(for: me) == .outgoing ? "Outgoing" : "Incoming"
        let duration = call.duration.map { " · \(Clock.duration($0))" } ?? ""
        return "\(direction) · \(call.outcome(for: me))\(duration)"
    }

    func refresh() async {
        defer { loaded = true }
        do {
            calls = try await services.calls.history(before: nil, limit: 50).calls
            error = nil
        } catch {
            self.error = error.code == .unavailable ? "The server is unreachable." : "Could not load calls."
            return
        }
        let peers = calls.flatMap { $0.participants.map(\.userID) + [$0.callerID] }.filter { $0 != me && !$0.isEmpty }
        if let profiles = try? await services.auth.profiles(ids: peers) {
            people.merge(profiles) { _, new in new }
        }
    }

    func scheduleRefresh() {
        pendingRefresh?.cancel()
        pendingRefresh = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(600))
            guard !Task.isCancelled else { return }
            await self?.refresh()
        }
    }

    func remember(_ profile: PublicProfile) {
        people[profile.id] = profile
    }

    func peer(of call: Call) -> String? {
        call.isGroup ? nil : call.peer(for: me)
    }

    func call(_ id: String) -> Call? {
        calls.first { $0.id == id }
    }

    func related(to call: Call) -> [Call] {
        calls.filter { other in
            guard other.isGroup == call.isGroup else { return false }
            return call.isGroup ? other.roomID == call.roomID : peer(of: other) == peer(of: call)
        }
    }

    func handle(of call: Call) -> String? {
        guard let peer = peer(of: call), let profile = people[peer], profile.hasUsername, !profile.username.isEmpty else { return nil }
        return "@\(profile.username)"
    }

    private func matches(_ call: Call) -> Bool {
        switch filter {
        case .all: true
        case .missed: call.direction(for: me) == .missed
        case .incoming: call.direction(for: me) != .outgoing
        case .outgoing: call.direction(for: me) == .outgoing
        case .audio: !call.isVideo
        case .video: call.isVideo
        }
    }
}
