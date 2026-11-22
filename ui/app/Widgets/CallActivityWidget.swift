import ActivityKit
import SwiftUI
import WidgetKit

struct CallActivityWidget: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: CallActivityAttributes.self) { context in
            HStack(spacing: 12) {
                Image(systemName: context.attributes.video ? "video.fill" : "phone.fill")
                    .font(.title3)
                    .foregroundStyle(WidgetPalette.success)
                VStack(alignment: .leading, spacing: 2) {
                    Text(context.attributes.name).font(.headline).lineLimit(1)
                    CallActivityStatus(state: context.state).font(.subheadline).foregroundStyle(.secondary)
                }
                Spacer()
                if context.state.muted {
                    Image(systemName: "mic.slash.fill").foregroundStyle(WidgetPalette.destructive)
                }
            }
            .padding(16)
            .widgetURL(URL(string: "axon://calls"))
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Image(systemName: context.attributes.video ? "video.fill" : "phone.fill")
                        .foregroundStyle(WidgetPalette.success)
                }
                DynamicIslandExpandedRegion(.center) {
                    Text(context.attributes.name).font(.headline).lineLimit(1)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    if context.state.muted {
                        Image(systemName: "mic.slash.fill").foregroundStyle(WidgetPalette.destructive)
                    }
                }
                DynamicIslandExpandedRegion(.bottom) {
                    CallActivityStatus(state: context.state).font(.subheadline).foregroundStyle(.secondary)
                }
            } compactLeading: {
                Image(systemName: "phone.fill").foregroundStyle(WidgetPalette.success)
            } compactTrailing: {
                if let connectedAt = context.state.connectedAt {
                    Text(connectedAt, style: .timer)
                        .font(.caption.monospacedDigit())
                        .frame(maxWidth: 48)
                } else {
                    Image(systemName: "ellipsis").foregroundStyle(.secondary)
                }
            } minimal: {
                Image(systemName: "phone.fill").foregroundStyle(WidgetPalette.success)
            }
            .widgetURL(URL(string: "axon://calls"))
        }
    }
}

struct CallActivityStatus: View {
    let state: CallActivityAttributes.ContentState

    var body: some View {
        if let connectedAt = state.connectedAt {
            Text(connectedAt, style: .timer)
        } else {
            Text(state.status)
        }
    }
}
