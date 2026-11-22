#if os(iOS)
import ActivityKit
import Foundation

struct CallActivityAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable, Sendable {
        var status: String
        var connectedAt: Date?
        var muted: Bool
    }

    var callID: String
    var name: String
    var video: Bool
}
#endif
