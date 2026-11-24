import SwiftUI

struct MessageActions {
    var reply: (ChatMessage) -> Void = { _ in }
    var edit: (ChatMessage) -> Void = { _ in }
    var forward: (ChatMessage) -> Void = { _ in }
    var delete: (ChatMessage) -> Void = { _ in }
    var select: (ChatMessage) -> Void = { _ in }
    var readers: (ChatMessage) -> Void = { _ in }
    var openMedia: (ChatMessage, String) -> Void = { _, _ in }
    var jump: (String) -> Void = { _ in }
}

struct MessageRow: View {
    let message: ChatMessage
    let startsSeries: Bool
    let endsSeries: Bool
    let model: ConversationViewModel
    let actions: MessageActions

    private var mine: Bool { model.isMine(message) }
    private var group: Bool { !model.isDirect }
    private var selecting: Bool { model.selection != nil }
    private var picked: Bool { model.selection?.contains(message.id) == true }

    var body: some View {
        if message.isSystem {
            Text(message.systemText(me: model.me, name: model.name(of:)))
                .font(.axCaption)
                .foregroundStyle(Theme.muted)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .background(Theme.secondary, in: .capsule)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 6)
                .accessibilityIdentifier(AccessibilityID.Chats.message(message.id))
        } else {
            HStack(alignment: .bottom, spacing: 8) {
                if selecting {
                    Image(systemName: picked ? "checkmark.circle.fill" : "circle")
                        .font(.system(size: 20))
                        .foregroundStyle(picked ? Theme.primary : Theme.muted)
                        .opacity(message.canSelect ? 1 : 0)
                        .padding(.bottom, 6)
                }
                if mine {
                    Spacer(minLength: 48)
                } else if group {
                    Group {
                        if endsSeries {
                            Avatar(
                                name: model.name(of: message.authorID),
                                url: model.hub.people[message.authorID]?.avatarLink,
                                size: 28,
                                cornerRadius: 14
                            )
                        } else {
                            Color.clear.frame(width: 28, height: 28)
                        }
                    }
                }
                VStack(alignment: mine ? .trailing : .leading, spacing: 3) {
                    if group, !mine, startsSeries {
                        Text(model.name(of: message.authorID))
                            .font(.geist(12, .semibold, relativeTo: .caption))
                            .foregroundStyle(Theme.primary)
                            .padding(.horizontal, 12)
                    }
                    bubble
                }
                if !mine { Spacer(minLength: 48) }
            }
            .padding(.top, startsSeries ? 6 : 1)
            .contentShape(Rectangle())
            .onTapGesture {
                if selecting { model.toggle(message) }
            }
            .accessibilityIdentifier(AccessibilityID.Chats.message(message.id))
        }
    }

    private var bubble: some View {
        MessageBubble(message: message, mine: mine, model: model, actions: actions)
            .overlay {
                RoundedRectangle(cornerRadius: 18, style: .continuous)
                    .fill(Theme.primary.opacity(model.highlighted == message.id ? 0.22 : 0))
                    .allowsHitTesting(false)
                    .animation(.easeOut(duration: 0.4), value: model.highlighted)
            }
            .contextMenu { if !selecting { menu } }
            .allowsHitTesting(!selecting)
    }

    @ViewBuilder
    private var menu: some View {
        if !message.isDeleted {
            Button("Reply", systemImage: "arrowshape.turn.up.left") { actions.reply(message) }
            if let text = message.copyableText {
                Button("Copy", systemImage: "doc.on.doc") { Clipboard.copy(text) }
            }
            if mine, message.isEditable {
                Button("Edit", systemImage: "pencil") { actions.edit(message) }
            }
            Button("Forward", systemImage: "arrowshape.turn.up.right") { actions.forward(message) }
            Button("Select", systemImage: "checkmark.circle") { actions.select(message) }
            if mine, group {
                Button("Seen by", systemImage: "eye") { actions.readers(message) }
            }
            if mine {
                Divider()
                Button("Delete", systemImage: "trash", role: .destructive) { actions.delete(message) }
            }
        }
    }
}

struct MessageBubble: View {
    let message: ChatMessage
    let mine: Bool
    let model: ConversationViewModel
    let actions: MessageActions

    private var mediaOnly: Bool {
        switch message.content {
        case .image, .video: message.caption.isEmpty && !message.hasReplyTo && !message.isForwarded
        default: false
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if message.isForwarded, !message.isDeleted {
                Label("Forwarded from \(model.name(of: message.forwardedFromAuthorID))", systemImage: "arrowshape.turn.up.right")
                    .font(.axCaption)
                    .foregroundStyle(Theme.muted)
            }
            if message.hasReplyTo, !message.isDeleted {
                ReplyQuote(author: model.name(of: message.replyTo.authorID), text: message.replyTo.summary)
                    .onTapGesture { actions.jump(message.replyTo.id) }
            }
            if message.content == .text {
                TextWithFooter(text: message.body) { footer }
            } else {
                content
            }
            if !message.caption.isEmpty, message.content != .text, !message.isDeleted {
                LinkedText(text: message.body)
            }
            if message.content != .text {
                footer.frame(maxWidth: .infinity, alignment: .trailing)
            }
        }
        .padding(.horizontal, mediaOnly ? 4 : 12)
        .padding(.vertical, mediaOnly ? 4 : 7)
        .background(mine ? Theme.outgoing : Theme.incoming, in: .rect(cornerRadius: 18, style: .continuous))
    }

    @ViewBuilder
    private var content: some View {
        switch message.content {
        case .text:
            LinkedText(text: message.body)
        case .deleted:
            Label("Message deleted", systemImage: "trash")
                .font(.axSubheadline.italic())
                .foregroundStyle(Theme.muted)
        case .voice(let voice):
            VoiceContent(id: message.id, voice: voice, tint: Theme.primary)
        case .file(let file):
            FileContent(name: file.filename, size: file.sizeBytes, raw: file.url)
        case .image, .video:
            if let item = MediaItem.items(of: message).first {
                MediaTile(item: item)
                    .frame(width: 240, height: 240 / item.aspect)
                    .clipShape(.rect(cornerRadius: 14, style: .continuous))
                    .onTapGesture { actions.openMedia(message, item.id) }
            }
        case .album(let items):
            AlbumContent(message: message, items: items, actions: actions)
        case .system:
            EmptyView()
        }
    }

    private var footer: some View {
        HStack(spacing: 4) {
            if message.isEdited {
                Text("edited")
            }
            Text(message.sentDate.formatted(date: .omitted, time: .shortened))
            if mine, !message.isDeleted {
                Image(systemName: model.isRead(message) ? "checkmark.circle.fill" : "checkmark")
                    .foregroundStyle(model.isRead(message) ? Theme.primary : Theme.muted)
                    .accessibilityLabel(model.isRead(message) ? "Read" : "Sent")
            }
        }
        .font(.axMono(10, relativeTo: .caption2))
        .foregroundStyle(Theme.muted)
        .padding(.horizontal, mediaOnly ? 6 : 0)
        .padding(.bottom, mediaOnly ? 2 : 0)
    }
}

struct AlbumContent: View {
    let message: ChatMessage
    let items: [AlbumItem]
    let actions: MessageActions

    var body: some View {
        let media = MediaItem.items(of: message)
        let files = items.filter { $0.kind == .attachment }
        VStack(alignment: .leading, spacing: 6) {
            if !media.isEmpty {
                LazyVGrid(columns: Array(repeating: GridItem(.fixed(118), spacing: 4), count: media.count == 1 ? 1 : 2), spacing: 4) {
                    ForEach(media) { item in
                        MediaTile(item: item)
                            .frame(width: media.count == 1 ? 240 : 118, height: media.count == 1 ? 240 / item.aspect : 118)
                            .clipShape(.rect(cornerRadius: 10, style: .continuous))
                            .onTapGesture { actions.openMedia(message, item.id) }
                    }
                }
                .frame(width: 240, alignment: .leading)
            }
            ForEach(files.indices, id: \.self) { index in
                FileContent(name: files[index].filename, size: files[index].sizeBytes, raw: files[index].url)
            }
        }
    }
}

struct ReplyQuote: View {
    let author: String
    let text: String

    var body: some View {
        HStack(spacing: 8) {
            Capsule().fill(Theme.primary).frame(width: 3)
            VStack(alignment: .leading, spacing: 1) {
                Text(author)
                    .font(.geist(12, .semibold, relativeTo: .caption))
                    .foregroundStyle(Theme.primary)
                Text(text)
                    .font(.axCaption)
                    .foregroundStyle(Theme.muted)
                    .lineLimit(2)
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .padding(.vertical, 2)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Replying to \(author): \(text)")
    }
}

struct LinkedText: View {
    let text: String

    var body: some View {
        Text(Self.attributed(text))
            .font(.axBody)
            .foregroundStyle(Theme.foreground)
            .tint(Theme.primary)
            .textSelection(.enabled)
            .fixedSize(horizontal: false, vertical: true)
    }

    static func attributed(_ text: String) -> AttributedString {
        var result = AttributedString(text)
        guard let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue) else { return result }
        let range = NSRange(text.startIndex..., in: text)
        for match in detector.matches(in: text, range: range) {
            guard let url = match.url, let swiftRange = Range(match.range, in: text),
                  let lower = AttributedString.Index(swiftRange.lowerBound, within: result),
                  let upper = AttributedString.Index(swiftRange.upperBound, within: result) else { continue }
            result[lower..<upper].link = url
            result[lower..<upper].underlineStyle = .single
        }
        return result
    }
}

struct DayDivider: View {
    let day: Date

    var body: some View {
        Text(ChatFormat.dayLabel(day))
            .font(.geist(12, .medium, relativeTo: .caption))
            .foregroundStyle(Theme.muted)
            .padding(.horizontal, 10)
            .padding(.vertical, 4)
            .background(Theme.card.opacity(0.9), in: .capsule)
            .overlay { Capsule().strokeBorder(Theme.border, lineWidth: 1) }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 8)
    }
}

struct OutgoingRow: View {
    let item: OutgoingMessage
    let model: ConversationViewModel

    var body: some View {
        HStack {
            Spacer(minLength: 48)
            VStack(alignment: .trailing, spacing: 4) {
                VStack(alignment: .leading, spacing: 6) {
                    if let quote = item.replyPreview {
                        ReplyQuote(author: model.name(of: quote.authorID), text: quote.summary)
                    }
                    if !item.draft.uploadIDs.isEmpty {
                        let count = item.draft.uploadIDs.count
                        Label(count == 1 ? "Attachment" : "\(count) attachments", systemImage: "paperclip")
                            .font(.axSubheadline)
                            .foregroundStyle(Theme.muted)
                    }
                    TextWithFooter(text: item.draft.body) {
                        HStack(spacing: 4) {
                            Text(item.createdAt.formatted(date: .omitted, time: .shortened))
                            status
                        }
                        .font(.axMono(10, relativeTo: .caption2))
                        .foregroundStyle(Theme.muted)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 7)
                .background(Theme.outgoing.opacity(0.7), in: .rect(cornerRadius: 18, style: .continuous))
                if case .failed(let reason) = item.status {
                    HStack(spacing: 8) {
                        Text(reason).foregroundStyle(Theme.destructive)
                        Button("Dismiss") { model.dismiss(item) }
                    }
                    .font(.axCaption)
                } else if item.status == .unconfirmed {
                    HStack(spacing: 10) {
                        Text("Not confirmed").foregroundStyle(Theme.warning)
                        Button("Retry") { Task { await model.retry(item) } }
                        Button("Dismiss") { model.dismiss(item) }
                    }
                    .font(.axCaption)
                }
            }
        }
        .padding(.top, 2)
        .accessibilityIdentifier(AccessibilityID.Chats.outgoing)
    }

    @ViewBuilder
    private var status: some View {
        switch item.status {
        case .sending, .unconfirmed:
            Image(systemName: "clock").accessibilityLabel(item.status == .sending ? "Sending" : "Delivery unconfirmed")
        case .sent:
            Image(systemName: "checkmark").accessibilityLabel("Sent")
        case .failed:
            Image(systemName: "exclamationmark.circle").foregroundStyle(Theme.destructive).accessibilityLabel("Not sent")
        }
    }
}
