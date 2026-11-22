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
    private var current: (activity: String, call: String)?

    func show(_ snapshot: CallActivitySnapshot?) {
        #if os(iOS)
        guard let snapshot else {
            end()
            return
        }
        let state = CallActivityAttributes.ContentState(status: snapshot.status, connectedAt: snapshot.connectedAt, muted: snapshot.muted)
        if let current, current.call == snapshot.callID {
            let id = current.activity
            Task.detached { await Self.activity(id)?.update(ActivityContent(state: state, staleDate: nil)) }
            return
        }
        end()
        guard ActivityAuthorizationInfo().areActivitiesEnabled else { return }
        let attributes = CallActivityAttributes(callID: snapshot.callID, name: snapshot.name, video: snapshot.video)
        if let activity = try? Activity.request(attributes: attributes, content: ActivityContent(state: state, staleDate: nil)) {
            current = (activity.id, snapshot.callID)
        }
        #endif
    }

    func end() {
        #if os(iOS)
        guard let current else { return }
        self.current = nil
        let id = current.activity
        Task.detached { await Self.activity(id)?.end(nil, dismissalPolicy: .immediate) }
        #endif
    }

    #if os(iOS)
    private nonisolated static func activity(_ id: String) -> Activity<CallActivityAttributes>? {
        Activity<CallActivityAttributes>.activities.first { $0.id == id }
    }
    #endif
}
