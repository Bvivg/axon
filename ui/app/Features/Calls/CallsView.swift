import SwiftUI

struct CallsView: View {
    @Bindable var model: CallsViewModel

    var body: some View {
        List {
            if let error = model.error {
                ErrorBanner(text: error).plainRow()
            }
            if !model.visible.isEmpty {
                Section {
                    ForEach(model.visible, id: \.id) { call in
                        CallRow(call: call, model: model)
                            .accessibilityIdentifier(AccessibilityID.Calls.call(call.id))
                            .plainRow()
                    }
                } header: {
                    Eyebrow(model.filter == .all ? "Recent" : model.filter.rawValue)
                }
            }
        }
        .listStyle(.plain)
        .themedList(Theme.sidebar)
        .overlay {
            if model.visible.isEmpty, model.error == nil {
                if !model.loaded {
                    ProgressView()
                } else if !model.query.isEmpty {
                    EmptyState(title: "Nothing matches “\(model.query)”", systemImage: "magnifyingglass")
                } else {
                    EmptyState(title: emptyTitle, systemImage: "phone")
                }
            }
        }
        .rootTitle("Calls")
        .searchField($model.query, prompt: "Search call history")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Picker("Show", selection: $model.filter) {
                        ForEach(CallFilter.allCases) { filter in
                            Text(filter.rawValue).tag(filter)
                        }
                    }
                } label: {
                    Label("Filter", systemImage: filterSymbol)
                }
                .accessibilityIdentifier(AccessibilityID.Calls.filter)
            }
        }
        .refreshable { await model.refresh() }
        .task { await model.refresh() }
        .accessibilityIdentifier(AccessibilityID.Calls.root)
    }

    private var emptyTitle: String {
        model.filter == .all ? "No calls yet" : "No \(model.filter.rawValue.lowercased()) calls"
    }

    private var filterSymbol: String {
        model.filter == .all ? "line.3.horizontal.decrease" : "line.3.horizontal.decrease.circle.fill"
    }
}

private struct CallRow: View {
    let call: Call
    let model: CallsViewModel

    var body: some View {
        let name = model.name(of: call)
        let missed = call.direction(for: model.me) == .missed
        HStack(spacing: 12) {
            Avatar(name: name, url: model.avatar(of: call), size: 44)
            VStack(alignment: .leading, spacing: 3) {
                HStack(alignment: .firstTextBaseline) {
                    Text(name)
                        .font(.geist(15, .semibold, relativeTo: .body))
                        .foregroundStyle(missed ? Theme.destructive : Theme.foreground)
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    Text(Clock.short(call.createdAt.date))
                        .font(.axMono(11, relativeTo: .caption2))
                        .foregroundStyle(Theme.muted)
                }
                HStack(spacing: 5) {
                    Image(systemName: symbol)
                        .font(.system(size: 11, weight: .semibold))
                    Text(model.summary(of: call))
                        .font(.axFootnote)
                        .lineLimit(1)
                    if call.isVideo {
                        Image(systemName: "video")
                            .font(.system(size: 11))
                            .accessibilityLabel("Video")
                    }
                }
                .foregroundStyle(missed ? Theme.destructive : Theme.muted)
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }

    private var symbol: String {
        switch call.direction(for: model.me) {
        case .missed: "phone.arrow.down.left.fill"
        case .outgoing: "phone.arrow.up.right"
        case .incoming: "phone.arrow.down.left"
        }
    }
}
