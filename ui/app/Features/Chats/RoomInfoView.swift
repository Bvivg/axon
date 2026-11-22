import SwiftUI

struct RoomInfoView: View {
    let roomID: String
    let hub: ChatsViewModel
    let onLeave: () -> Void
    @State private var details: RoomDetails?
    @State private var error: String?
    @State private var renaming = false
    @State private var newName = ""
    @State private var adding = false
    @State private var additions: [PublicProfile] = []
    @State private var confirmsLeave = false
    @State private var removing: RoomMember?

    private var room: Room? { hub.room(roomID) ?? details?.room }
    private var direct: Bool { room?.isDirect == true }
    private var owner: Bool { details?.members.first { $0.userID == hub.me }?.role == .owner }

    var body: some View {
        ProfileScroll(title: direct ? "Contact info" : "Group info") {
            header
            if let error {
                ErrorBanner(text: error)
            }
            if !direct, let details {
                ProfileSection("Members", content: {
                    VStack(spacing: 0) {
                        if owner {
                            Button {
                                additions = []
                                adding = true
                            } label: {
                                Label("Add members", systemImage: "person.badge.plus")
                                    .font(.geist(15, .medium, relativeTo: .body))
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .padding(.vertical, 10)
                            }
                            .buttonStyle(.plain)
                            .foregroundStyle(Theme.primary)
                            .accessibilityIdentifier(AccessibilityID.Chats.addMembers)
                            Divider().overlay(Theme.line)
                        }
                        ForEach(details.members, id: \.userID) { member in
                            memberRow(member)
                            if member.userID != details.members.last?.userID {
                                Divider().overlay(Theme.line)
                            }
                        }
                    }
                }, aside: {
                    Text(verbatim: "\(details.members.count)")
                        .font(.axMono(12, relativeTo: .caption))
                        .foregroundStyle(Theme.muted)
                })
            }
            ProfileSection("Shared") {
                NavigationLink(value: Route.roomMedia(roomID)) {
                    CatalogRow(title: "Media, files and voice") {
                        IconTile(size: 36) { Image(systemName: "photo.on.rectangle").font(.system(size: 15)) }
                    }
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier(AccessibilityID.Chats.mediaLibrary)
            }
            Button(role: .destructive) {
                confirmsLeave = true
            } label: {
                Label(direct ? "Delete chat" : "Leave group", systemImage: direct ? "trash" : "rectangle.portrait.and.arrow.right")
                    .font(.geist(15, .medium, relativeTo: .body))
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, 12)
                    .padding(.horizontal, 16)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .foregroundStyle(Theme.destructive)
            .surface(padding: 0)
            .accessibilityIdentifier(AccessibilityID.Chats.leave)
            .confirmationDialog(
                direct ? "Delete this chat from your list?" : "Leave this group?",
                isPresented: $confirmsLeave,
                titleVisibility: .visible
            ) {
                Button(direct ? "Delete chat" : "Leave group", role: .destructive) { Task { await leave() } }
            }
        }
        .task { await load() }
        .alert("Rename group", isPresented: $renaming) {
            TextField("Group name", text: $newName)
            Button("Cancel", role: .cancel) {}
            Button("Save") { Task { await rename() } }
        }
        .sheet(isPresented: $adding) {
            NavigationStack {
                PeoplePicker(hub: hub, multiple: true, excluded: Set(details?.members.map(\.userID) ?? []), picked: $additions)
                    .navigationTitle("Add members")
                    #if os(iOS)
                    .navigationBarTitleDisplayMode(.inline)
                    #endif
                    .toolbar {
                        ToolbarItem(placement: .cancellationAction) {
                            Button("Close", systemImage: "xmark") { adding = false }
                        }
                        ToolbarItem(placement: .confirmationAction) {
                            Button("Add") { Task { await add() } }
                                .disabled(additions.isEmpty)
                                .accessibilityIdentifier(AccessibilityID.Chats.confirmAdd)
                        }
                    }
            }
            .presentationBackground(Theme.popover)
        }
    }

    private var header: some View {
        let name = room.map(hub.name(of:)) ?? "Chat"
        let peer = direct ? room.map(\.peerUserID).flatMap { hub.people[$0] } : nil
        return VStack(spacing: 10) {
            Avatar(name: name, url: room.flatMap(hub.avatar(of:)), size: 84, cornerRadius: 42)
            HStack(spacing: 6) {
                Text(name)
                    .font(.axTitle3)
                    .foregroundStyle(Theme.foreground)
                if !direct, owner {
                    Button("Rename", systemImage: "pencil") {
                        newName = room?.name ?? ""
                        renaming = true
                    }
                    .labelStyle(.iconOnly)
                    .foregroundStyle(Theme.primary)
                    .accessibilityIdentifier(AccessibilityID.Chats.rename)
                }
            }
            if let peer {
                if peer.hasUsername, !peer.username.isEmpty {
                    Text("@\(peer.username)").font(.axSubheadline).foregroundStyle(Theme.muted)
                }
                if let seen = ChatFormat.lastSeen(peer) {
                    StatusBadge(text: seen, tone: peer.online ? .success : .neutral, dot: peer.online)
                }
            }
        }
        .frame(maxWidth: .infinity)
        .surface(padding: 20)
    }

    private func memberRow(_ member: RoomMember) -> some View {
        let profile = hub.people[member.userID]
        return HStack(spacing: 12) {
            Avatar(name: hub.name(ofUser: member.userID), url: profile?.avatarLink, size: 36, cornerRadius: 18)
            VStack(alignment: .leading, spacing: 1) {
                Text(hub.name(ofUser: member.userID))
                    .font(.geist(15, .medium, relativeTo: .body))
                    .foregroundStyle(Theme.foreground)
                if let seen = ChatFormat.lastSeen(profile) {
                    Text(seen).font(.axCaption).foregroundStyle(profile?.online == true ? Theme.success : Theme.muted)
                }
            }
            Spacer()
            if member.role == .owner {
                StatusBadge(text: "Owner", tone: .signal)
            } else if owner {
                Button("Remove", systemImage: "minus.circle") { removing = member }
                    .labelStyle(.iconOnly)
                    .foregroundStyle(Theme.destructive)
                    .buttonStyle(.plain)
                    .accessibilityIdentifier(AccessibilityID.Chats.removeMember(member.userID))
                    .confirmationDialog(
                        "Remove \(hub.name(ofUser: member.userID)) from the group?",
                        isPresented: Binding(
                            get: { removing?.userID == member.userID },
                            set: { if !$0 { removing = nil } }
                        ),
                        titleVisibility: .visible
                    ) {
                        Button("Remove", role: .destructive) { Task { await remove(member) } }
                    }
            }
        }
        .padding(.vertical, 8)
        .contentShape(Rectangle())
        .onTapGesture {
            if member.userID != hub.me { hub.openPerson(member.userID) }
        }
    }

    private func load() async {
        do {
            let details = try await hub.services.chat.room(id: roomID)
            self.details = details
            await hub.ensureProfiles(details.members.map(\.userID))
            error = nil
        } catch {
            self.error = ConversationViewModel.message(error)
        }
    }

    private func rename() async {
        let name = newName.trimmingCharacters(in: .whitespaces)
        guard !name.isEmpty else { return }
        do {
            hub.adopt(try await hub.services.chat.renameGroup(roomID, to: name))
            await load()
        } catch {
            self.error = ConversationViewModel.message(error)
        }
    }

    private func add() async {
        do {
            try await hub.services.chat.addMembers(additions.map(\.id), to: roomID)
            adding = false
            await load()
        } catch {
            self.error = ConversationViewModel.message(error)
        }
    }

    private func remove(_ member: RoomMember) async {
        removing = nil
        do {
            try await hub.services.chat.removeMember(member.userID, from: roomID)
            await load()
        } catch {
            self.error = ConversationViewModel.message(error)
        }
    }

    private func leave() async {
        do {
            if direct {
                try await hub.services.chat.hide(room: roomID)
            } else {
                try await hub.services.chat.leave(room: roomID)
            }
            hub.forget(roomID)
            onLeave()
        } catch {
            self.error = ConversationViewModel.message(error)
        }
    }
}

struct RoomMediaView: View {
    enum Section: String, CaseIterable, Identifiable {
        case media = "Media"
        case files = "Files"
        case voice = "Voice"

        var id: String { rawValue }

        var kinds: [Axon_Chat_V1_MessageKind] {
            switch self {
            case .media: [.image, .video, .album]
            case .files: [.attachment, .album]
            case .voice: [.voice]
            }
        }
    }

    let roomID: String
    let hub: ChatsViewModel
    @State private var section = Section.media
    @State private var messages: [ChatMessage] = []
    @State private var hasMore = false
    @State private var loading = false
    @State private var viewer: ConversationView.ViewerState?
    @Environment(\.media) private var media

    private var mediaItems: [MediaItem] { messages.flatMap(MediaItem.items(of:)) }

    private var files: [(String, Axon_Chat_V1_AttachmentPayload)] {
        messages.flatMap { message -> [(String, Axon_Chat_V1_AttachmentPayload)] in
            switch message.content {
            case .file(let file): return [(message.id, file)]
            case .album(let items):
                return items.enumerated().filter { $0.element.kind == .attachment }.map { index, item in
                    ("\(message.id)#\(index)", Axon_Chat_V1_AttachmentPayload.with {
                        $0.url = item.url
                        $0.filename = item.filename
                        $0.mime = item.mime
                        $0.sizeBytes = item.sizeBytes
                    })
                }
            default: return []
            }
        }
    }

    var body: some View {
        ScrollView {
            VStack(spacing: 12) {
                Picker("Section", selection: $section) {
                    ForEach(Section.allCases) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
                .accessibilityIdentifier(AccessibilityID.Chats.mediaSection)
                switch section {
                case .media:
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 104), spacing: 3)], spacing: 3) {
                        ForEach(mediaItems) { item in
                            Color.clear
                                .aspectRatio(1, contentMode: .fit)
                                .overlay { MediaTile(item: item) }
                                .clipShape(.rect(cornerRadius: 6))
                                .contentShape(Rectangle())
                                .onTapGesture { viewer = .init(items: mediaItems, selection: item.id) }
                        }
                    }
                case .files:
                    LazyVStack(alignment: .leading, spacing: 10) {
                        ForEach(files, id: \.0) { entry in
                            FileContent(name: entry.1.filename, size: entry.1.sizeBytes, raw: entry.1.url)
                        }
                    }
                case .voice:
                    LazyVStack(alignment: .leading, spacing: 12) {
                        ForEach(messages, id: \.id) { message in
                            if case .voice(let voice) = message.content {
                                VStack(alignment: .leading, spacing: 4) {
                                    Text("\(hub.name(ofUser: message.authorID)) · \(Clock.short(message.sentDate))")
                                        .font(.axCaption)
                                        .foregroundStyle(Theme.muted)
                                    VoiceContent(id: message.id, voice: voice, tint: Theme.primary)
                                }
                            }
                        }
                    }
                }
                if hasMore {
                    ProgressView()
                        .padding()
                        .onAppear { Task { await load(more: true) } }
                }
            }
            .padding(16)
        }
        .overlay {
            if !loading, messages.isEmpty {
                EmptyState(title: "Nothing shared yet", systemImage: "tray")
            }
        }
        .background(Theme.background)
        .navigationTitle("Shared")
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .task(id: section) { await load(more: false) }
        .sheet(item: $viewer) { state in
            MediaViewer(items: state.items, selection: state.selection)
                .environment(\.media, media)
        }
    }

    private func load(more: Bool) async {
        guard !loading else { return }
        loading = true
        defer { loading = false }
        if !more { messages = [] }
        let before = more ? messages.map(\.seq).min() : nil
        let query = HistoryQuery(before: before, limit: 60, kinds: section.kinds, groupedItems: true)
        guard let page = try? await hub.services.chat.messages(in: roomID, query) else { return }
        messages = (messages + page.messages.sorted { $0.seq > $1.seq }).filter { !$0.isDeleted }
        hasMore = page.hasMore
        await hub.ensureProfiles(page.messages.map(\.authorID))
    }
}
