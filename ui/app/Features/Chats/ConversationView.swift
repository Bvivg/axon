import SwiftUI

struct ConversationView: View {
    @State private var model: ConversationViewModel
    @State private var atBottom = true
    @State private var tracker = FeedTracker()
    @State private var viewer: ViewerState?
    @State private var forwarding: ForwardRequest?
    @State private var readersOf: ChatMessage?
    @State private var searching = false
    @State private var deleting: [String]?
    @Environment(\.media) private var media
    @Environment(\.scenePhase) private var scenePhase

    struct ViewerState: Identifiable {
        let items: [MediaItem]
        let selection: String
        var id: String { selection }
    }

    static let bottom = "feed-bottom"
    static let nearBottom: CGFloat = 120
    static let slack: CGFloat = 24

    struct FeedEdge: Equatable {
        let offset: CGFloat
        let gap: CGFloat
    }

    struct ForwardRequest: Identifiable {
        let messages: [String]
        var id: String { messages.joined(separator: ",") }
    }

    init(target: ChatTarget, hub: ChatsViewModel) {
        _model = State(initialValue: ConversationViewModel(target: target, hub: hub))
    }

    var body: some View {
        feed
            .background(Theme.background)
            .overlay(alignment: .bottomTrailing) { jumpButton }
            .safeAreaInset(edge: .bottom) {
                bottomBar.confirmationDialog(deleteTitle, isPresented: confirmingDelete, titleVisibility: .visible) {
                    Button("Delete", role: .destructive) {
                        let ids = deleting ?? []
                        deleting = nil
                        Task { await model.delete(ids) }
                    }
                }
            }
            .overlay(alignment: .top) { ConversationBanners(model: model) }
            .navigationTitle(model.title)
            .navigationSubtitle(model.subtitle ?? "")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            .toolbarVisibility(.hidden, for: .tabBar)
            #endif
            .toolbar { toolbar }
            .task { await model.start() }
            .onAppear { model.appear() }
            .onDisappear { model.disappear() }
            .onChange(of: scenePhase) { _, phase in
                if phase == .active { Task { await model.markRead() } }
            }
            .onChange(of: model.scrollTarget) { _, target in
                guard let target else { return }
                atBottom = false
                withAnimation(.snappy) { tracker.proxy?.scrollTo(target, anchor: .center) }
                model.scrollTarget = nil
            }
            .onChange(of: model.rows.last?.id) { _, _ in followBottom() }
            .onChange(of: model.pendingOutgoing.count) { old, new in
                guard new > old else { return }
                atBottom = true
                withAnimation(.snappy) { scrollToBottom() }
            }
            .onChange(of: model.firstUnread) { _, first in
                guard let first else { return }
                atBottom = false
                tracker.proxy?.scrollTo(first, anchor: .top)
            }
            .sheet(item: $viewer) { state in
                MediaViewer(items: state.items, selection: state.selection)
                    .environment(\.media, media)
            }
            .sheet(item: $forwarding) { request in
                ForwardSheet(hub: model.hub, count: request.messages.count) { rooms, comment in
                    await model.forward(request.messages, to: rooms, comment: comment)
                }
            }
            .sheet(item: $readersOf) { message in
                ReadersSheet(model: model, message: message)
            }
            .sheet(isPresented: $searching) {
                MessageSearchSheet(model: model) { message in
                    searching = false
                    Task { await model.jump(to: message) }
                }
            }
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier(AccessibilityID.Chats.conversation)
    }

    private var feedScroll: some View {
        ScrollView {
            LazyVStack(spacing: 0) {
                if model.hasOlder {
                    ProgressView()
                        .padding(12)
                        .frame(maxWidth: .infinity)
                        .onAppear { Task { await model.loadOlder() } }
                }
                if model.loaded, model.rows.isEmpty, model.pendingOutgoing.isEmpty, model.sendingVoice == 0 {
                    EmptyState(title: model.roomID == nil ? "Say hello" : "No messages yet", systemImage: "message")
                        .padding(.top, 80)
                }
                ForEach(model.rows) { row in
                    switch row.kind {
                    case .day(let day):
                        DayDivider(day: day)
                    case .message(let message, let starts, let ends):
                        MessageRow(message: message, startsSeries: starts, endsSeries: ends, model: model, actions: actions)
                    }
                }
                ForEach(model.pendingOutgoing) { item in
                    OutgoingRow(item: item, model: model)
                }
                ForEach(0..<model.sendingVoice, id: \.self) { _ in
                    HStack {
                        Spacer()
                        Label("Sending voice message…", systemImage: "waveform")
                            .font(.axFootnote)
                            .foregroundStyle(Theme.muted)
                            .padding(10)
                            .background(Theme.outgoing.opacity(0.6), in: .rect(cornerRadius: 18, style: .continuous))
                    }
                    .padding(.top, 2)
                }
                Color.clear
                    .frame(height: 1)
                    .id(Self.bottom)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .defaultScrollAnchor(.bottom, for: .initialOffset)
        .onScrollGeometryChange(for: FeedEdge.self) { geometry in
            FeedEdge(
                offset: geometry.contentOffset.y,
                gap: geometry.contentSize.height - geometry.contentOffset.y - geometry.contentInsets.top - geometry.containerSize.height
            )
        } action: { old, new in
            tracker.gap = new.gap
            if FeedTracker.isUser(tracker.phase) {
                atBottom = new.gap <= Self.nearBottom
            } else if atBottom, new.offset == old.offset, new.gap > old.gap + 1 {
                scrollToBottom()
            }
        }
        .onScrollPhaseChange { old, new in
            tracker.phase = new
            if old == .animating, new == .idle { settle() }
        }
        .scrollDismissesKeyboard(.interactively)
        .accessibilityIdentifier(AccessibilityID.Chats.feed)
    }

    private var actions: MessageActions {
        MessageActions(
            reply: { model.reply(to: $0) },
            edit: { model.startEdit($0) },
            forward: { forwarding = ForwardRequest(messages: [$0.id]) },
            delete: { deleting = [$0.id] },
            select: { message in
                model.selection = []
                model.toggle(message)
            },
            readers: { readersOf = $0 },
            openMedia: { message, item in open(message, item) },
            jump: { id in
                if let message = model.message(id: id) {
                    Task { await model.jump(to: message) }
                }
            }
        )
    }

    @ViewBuilder
    private var jumpButton: some View {
        if !atBottom, !model.rows.isEmpty {
            Button {
                atBottom = true
                withAnimation(.snappy) { scrollToBottom() }
            } label: {
                Image(systemName: "chevron.down")
                    .font(.system(size: 15, weight: .semibold))
                    .frame(width: 40, height: 40)
            }
            .buttonStyle(.glass)
            .buttonBorderShape(.circle)
            .padding(.trailing, 16)
            .padding(.bottom, 8)
            .accessibilityLabel("Scroll to latest")
        }
    }

    @ViewBuilder
    private var bottomBar: some View {
        if model.removed {
            Label("You are no longer a member of this chat.", systemImage: "person.crop.circle.badge.xmark")
                .font(.axFootnote)
                .foregroundStyle(Theme.muted)
                .padding(14)
                .frame(maxWidth: .infinity)
                .glassEffect(.regular, in: .rect(cornerRadius: 22, style: .continuous))
                .padding(10)
        } else if model.selection != nil {
            SelectionBar(model: model) { ids in
                forwarding = ForwardRequest(messages: ids)
            } delete: { ids in
                deleting = ids
            }
        } else {
            ComposerView(model: model)
        }
    }
}

final class FeedTracker {
    var phase: ScrollPhase = .idle
    var gap: CGFloat = 0
    var laidOut = false
    var proxy: ScrollViewProxy?

    static func isUser(_ phase: ScrollPhase) -> Bool {
        phase == .tracking || phase == .interacting || phase == .decelerating
    }
}

extension ConversationView {
    @ToolbarContentBuilder
    fileprivate var toolbar: some ToolbarContent {
        if model.selection != nil {
            ToolbarItem(placement: .confirmationAction) {
                Button("Done") { model.selection = nil }
            }
        } else if let room = model.roomID {
            ToolbarItem(placement: .primaryAction) {
                CallMenu(target: model.isDirect ? .room(room) : .group(room), name: callName, group: !model.isDirect)
            }
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button("Search", systemImage: "magnifyingglass") { searching = true }
                    NavigationLink(value: Route.roomMedia(room)) {
                        Label("Media, files and voice", systemImage: "photo.on.rectangle")
                    }
                    Button("Select messages", systemImage: "checkmark.circle") { model.selection = [] }
                    NavigationLink(value: Route.roomInfo(room)) {
                        Label(model.isDirect ? "Contact info" : "Group info", systemImage: "info.circle")
                    }
                } label: {
                    Label("More", systemImage: "ellipsis")
                }
                .accessibilityIdentifier(AccessibilityID.Chats.more)
            }
            ToolbarItem(placement: .primaryAction) {
                NavigationLink(value: Route.roomInfo(room)) {
                    Avatar(name: model.title, url: model.avatar, size: 32, cornerRadius: 16)
                }
                .accessibilityLabel("Chat info")
                .accessibilityIdentifier(AccessibilityID.Chats.info)
            }
            .sharedBackgroundVisibility(.hidden)
        } else if let peer = model.peerID {
            ToolbarItem(placement: .primaryAction) {
                CallMenu(target: .person(peer), name: model.title)
            }
        }
    }

    fileprivate var callName: String { model.isDirect ? model.title : "" }

    fileprivate var feed: some View {
        ScrollViewReader { proxy in
            feedScroll.onAppear { tracker.proxy = proxy }
        }
    }

    fileprivate var deleteTitle: String {
        let count = deleting?.count ?? 0
        return count > 1 ? "Delete \(count) messages for everyone?" : "Delete this message for everyone?"
    }

    fileprivate var confirmingDelete: Binding<Bool> {
        Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } })
    }

    fileprivate func scrollToBottom() {
        tracker.proxy?.scrollTo(model.pendingOutgoing.last?.id ?? model.rows.last?.id ?? Self.bottom, anchor: .bottom)
    }

    fileprivate func settle() {
        guard atBottom else {
            if tracker.gap <= Self.slack { atBottom = true }
            return
        }
        guard tracker.gap > Self.slack else { return }
        scrollToBottom()
    }

    fileprivate func followBottom() {
        guard atBottom else { return }
        guard tracker.laidOut else {
            tracker.laidOut = true
            scrollToBottom()
            return
        }
        withAnimation(.snappy) { scrollToBottom() }
    }

    fileprivate func open(_ message: ChatMessage, _ item: String) {
        let items = model.timeline.messages.flatMap(MediaItem.items(of:))
        guard !items.isEmpty else { return }
        viewer = ViewerState(items: items, selection: item)
    }
}

private struct SelectionBar: View {
    let model: ConversationViewModel
    let forward: ([String]) -> Void
    let delete: ([String]) -> Void

    var body: some View {
        let picked = model.selectedMessages
        HStack(spacing: 16) {
            Text(picked.isEmpty ? "Select messages" : "\(picked.count) selected")
                .font(.geist(14, .medium, relativeTo: .subheadline))
                .foregroundStyle(Theme.foreground)
            Spacer()
            Button("Copy", systemImage: "doc.on.doc") {
                Clipboard.copy(picked.compactMap(\.copyableText).joined(separator: "\n\n"))
                model.selection = nil
            }
            .disabled(picked.allSatisfy { $0.copyableText == nil })
            Button("Forward", systemImage: "arrowshape.turn.up.right") { forward(picked.map(\.id)) }
                .disabled(picked.isEmpty)
                .accessibilityIdentifier(AccessibilityID.Chats.forwardSelection)
            Button("Delete", systemImage: "trash", role: .destructive) { delete(picked.map(\.id)) }
                .disabled(!model.canDeleteSelection)
                .accessibilityIdentifier(AccessibilityID.Chats.deleteSelection)
        }
        .labelStyle(.iconOnly)
        .font(.system(size: 18))
        .padding(.horizontal, 18)
        .padding(.vertical, 12)
        .glassEffect(.regular, in: .rect(cornerRadius: 24, style: .continuous))
        .padding(.horizontal, 10)
        .padding(.bottom, 6)
    }
}

private struct ConversationBanners: View {
    @Bindable var model: ConversationViewModel

    var body: some View {
        VStack(spacing: 6) {
            if !model.hub.connected, model.loaded {
                Label(model.hub.connection == .closed ? "Offline" : "Connecting…", systemImage: "wifi.exclamationmark")
                    .font(.axCaption)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 5)
                    .glassEffect(.regular, in: .capsule)
            }
            if let error = model.error {
                ErrorBanner(text: error).padding(.horizontal, 12)
            }
            if let notice = model.notice {
                Text(notice)
                    .font(.axFootnote)
                    .foregroundStyle(Theme.foreground)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .glassEffect(.regular, in: .capsule)
                    .onTapGesture { model.notice = nil }
                    .task(id: notice) {
                        try? await Task.sleep(for: .seconds(4))
                        if model.notice == notice { model.notice = nil }
                    }
                    .accessibilityIdentifier(AccessibilityID.Chats.notice)
            }
        }
        .padding(.top, 6)
        .animation(.snappy, value: model.notice)
    }
}
