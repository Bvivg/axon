import Foundation
#if os(iOS)
import ActivityKit
#endif

struct CallActivitySnapshot: Equatable, Sendable {
    let callID: String
    let name: String
    let video: Bool
    let status: String
    let connectedAt: Date?
    let muted: Bool
}

extension CallEngine {
    var activitySnapshot: CallActivitySnapshot? {
        guard let call, call.state != .ended, !(isIncoming && call.state == .ringing) else { return nil }
        let status: String = switch call.state {
        case .ringing: "Ringing…"
        case .connecting: "Connecting…"
        case .active: disconnected || reconnecting ? "Reconnecting…" : "Connected"
        default: ""
        }
        return CallActivitySnapshot(
            callID: call.id,
            name: CallScreen.title(call, engine: self),
            video: call.isVideo,
            status: status,
            connectedAt: call.state == .active && !(disconnected || reconnecting) ? call.connectedSince : nil,
            muted: media?.muted == true
        )
    }
}

@MainActor
final class CallActivities {
    func show(_ snapshot: CallActivitySnapshot?) {
        #if os(iOS)
        guard let snapshot else {
            end()
            return
        }
        let call = snapshot.callID
        Self.endAll(except: call)
        let state = CallActivityAttributes.ContentState(status: snapshot.status, connectedAt: snapshot.connectedAt, muted: snapshot.muted)
        if let id = Activity<CallActivityAttributes>.activities.first(where: { $0.attributes.callID == call })?.id {
            Task.detached { await Self.activity(id)?.update(ActivityContent(state: state, staleDate: nil)) }
            return
        }
        guard ActivityAuthorizationInfo().areActivitiesEnabled else { return }
        let attributes = CallActivityAttributes(callID: call, name: snapshot.name, video: snapshot.video)
        _ = try? Activity.request(attributes: attributes, content: ActivityContent(state: state, staleDate: nil))
        #endif
    }

    func end() {
        #if os(iOS)
        Self.endAll(except: nil)
        #endif
    }

    #if os(iOS)
    private nonisolated static func endAll(except call: String?) {
        let ids = Activity<CallActivityAttributes>.activities.filter { $0.attributes.callID != call }.map(\.id)
        guard !ids.isEmpty else { return }
        Task.detached {
            for id in ids { await activity(id)?.end(nil, dismissalPolicy: .immediate) }
        }
    }

    private nonisolated static func activity(_ id: String) -> Activity<CallActivityAttributes>? {
        Activity<CallActivityAttributes>.activities.first { $0.id == id }
    }
    #endif
}
