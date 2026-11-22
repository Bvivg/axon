import PhotosUI
import SwiftUI
import UniformTypeIdentifiers

struct ComposerView: View {
    @Bindable var model: ConversationViewModel
    @State private var recorder = VoiceRecorder()
    @State private var picking = false
    @State private var photos: [PhotosPickerItem] = []
    @State private var importing = false
    @FocusState private var focused: Bool

    private var empty: Bool {
        model.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && model.tray.isEmpty && model.editing == nil
    }

    var body: some View {
        VStack(spacing: 8) {
            if let editing = model.editing {
                ComposerContext(title: "Edit message", text: editing.preview, systemImage: "pencil") { model.cancelEdit() }
            } else if let reply = model.replyTo {
                ComposerContext(
                    title: "Reply to \(model.name(of: reply.authorID))",
                    text: reply.preview,
                    systemImage: "arrowshape.turn.up.left"
                ) {
                    model.replyTo = nil
                }
            }
            if !model.tray.isEmpty {
                ScrollView(.horizontal) {
                    HStack(spacing: 8) {
                        ForEach(model.tray.items) { item in
                            AttachmentChip(item: item, model: model)
                        }
                    }
                }
                .scrollIndicators(.hidden)
            }
            HStack(alignment: .bottom, spacing: 8) {
                if recorder.recording {
                    recording
                } else {
                    editor
                }
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .glassEffect(.regular, in: .rect(cornerRadius: 26, style: .continuous))
        .padding(.horizontal, 10)
        .padding(.bottom, 6)
        .photosPicker(
            isPresented: $picking,
            selection: $photos,
            maxSelectionCount: AttachmentTray.limit,
            matching: .any(of: [.images, .videos])
        )
        .onChange(of: photos) { _, items in
            guard !items.isEmpty else { return }
            photos = []
            Task { await load(items) }
        }
        .fileImporter(isPresented: $importing, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case .success(let urls) = result else { return }
            model.attach(urls.compactMap { Staging.copy($0) }, as: .file)
        }
        .onChange(of: recorder.denied) { _, denied in
            if denied { model.notice = "Allow microphone access in Settings to record voice messages." }
        }
        .onChange(of: model.editing?.id) { _, editing in
            if editing != nil { focused = true }
        }
        .onChange(of: model.replyTo?.id) { _, reply in
            if reply != nil { focused = true }
        }
    }

    private var editor: some View {
        Group {
            Menu {
                Button("Photos & videos", systemImage: "photo.on.rectangle") { picking = true }
                Button("File", systemImage: "doc") { importing = true }
            } label: {
                Image(systemName: "plus")
                    .font(.system(size: 18, weight: .medium))
                    .frame(width: 36, height: 36)
            }
            .disabled(model.editing != nil)
            .accessibilityLabel("Attach")
            .accessibilityIdentifier(AccessibilityID.Chats.attach)
            TextField("Message", text: $model.draft, axis: .vertical)
                .font(.axBody)
                .lineLimit(1...6)
                .focused($focused)
                .padding(.vertical, 8)
                .accessibilityIdentifier(AccessibilityID.Chats.composer)
            if empty {
                Button {
                    AudioPlayback.shared.stop()
                    Task { await recorder.start() }
                } label: {
                    Image(systemName: "mic")
                        .font(.system(size: 18, weight: .medium))
                        .frame(width: 36, height: 36)
                }
                .accessibilityLabel("Record a voice message")
                .accessibilityIdentifier(AccessibilityID.Chats.record)
            } else {
                Button {
                    Task { await model.send() }
                } label: {
                    Image(systemName: model.editing == nil ? "arrow.up" : "checkmark")
                        .font(.system(size: 16, weight: .bold))
                        .foregroundStyle(Theme.onPrimary)
                        .frame(width: 36, height: 36)
                        .background(model.canSend ? Theme.primary : Theme.muted.opacity(0.5), in: .circle)
                }
                .buttonStyle(.plain)
                .disabled(!model.canSend)
                .keyboardShortcut(.return, modifiers: .command)
                .accessibilityLabel(model.editing == nil ? "Send" : "Save")
                .accessibilityIdentifier(AccessibilityID.Chats.send)
            }
        }
    }

    private var recording: some View {
        Group {
            Button {
                recorder.cancel()
            } label: {
                Image(systemName: "trash")
                    .font(.system(size: 17))
                    .foregroundStyle(Theme.destructive)
                    .frame(width: 36, height: 36)
            }
            .accessibilityLabel("Discard recording")
            HStack(spacing: 8) {
                Circle().fill(Theme.destructive).frame(width: 8, height: 8)
                    .opacity(0.4 + Double(recorder.level) * 0.6)
                Text(Clock.duration(.seconds(recorder.elapsed)))
                    .font(.axMono(14, relativeTo: .body))
                    .foregroundStyle(Theme.foreground)
                Capsule()
                    .fill(Theme.primary.opacity(0.35))
                    .frame(maxWidth: .infinity)
                    .frame(height: 4)
                    .overlay(alignment: .leading) {
                        Capsule().fill(Theme.primary)
                            .frame(width: 40 + CGFloat(recorder.level) * 120, height: 4)
                    }
            }
            .frame(maxWidth: .infinity, minHeight: 36)
            Button {
                if let (file, _) = recorder.finish() {
                    model.sendVoice(file)
                }
            } label: {
                Image(systemName: "arrow.up")
                    .font(.system(size: 16, weight: .bold))
                    .foregroundStyle(Theme.onPrimary)
                    .frame(width: 36, height: 36)
                    .background(Theme.primary, in: .circle)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Send voice message")
        }
    }

    private func load(_ items: [PhotosPickerItem]) async {
        var urls: [URL] = []
        for item in items {
            if let picked = try? await item.loadTransferable(type: PickedMedia.self) {
                urls.append(picked.url)
            }
        }
        if urls.count < items.count { model.notice = "Some items could not be attached." }
        model.attach(urls, as: .media)
    }
}

private struct ComposerContext: View {
    let title: String
    let text: String
    let systemImage: String
    let cancel: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: systemImage)
                .foregroundStyle(Theme.primary)
            VStack(alignment: .leading, spacing: 1) {
                Text(title)
                    .font(.geist(12, .semibold, relativeTo: .caption))
                    .foregroundStyle(Theme.primary)
                Text(text)
                    .font(.axCaption)
                    .foregroundStyle(Theme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 0)
            Button("Cancel", systemImage: "xmark.circle.fill", action: cancel)
                .labelStyle(.iconOnly)
                .foregroundStyle(Theme.muted)
                .buttonStyle(.plain)
        }
        .padding(.horizontal, 4)
    }
}

private struct AttachmentChip: View {
    let item: PendingAttachment
    let model: ConversationViewModel
    @State private var thumbnail: CGImage?

    var body: some View {
        ZStack(alignment: .topTrailing) {
            VStack(spacing: 4) {
                ZStack {
                    RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Theme.secondary)
                    if item.isImage, let image = thumbnail {
                        Image(decorative: image, scale: 1).resizable().scaledToFill()
                    } else {
                        Image(systemName: item.isVideo ? "video" : "doc")
                            .font(.system(size: 20))
                            .foregroundStyle(Theme.primary)
                    }
                    if case .failed = item.phase {
                        Color.black.opacity(0.45)
                        Button("Retry", systemImage: "arrow.clockwise") { model.tray.retry(item.id) }
                            .labelStyle(.iconOnly)
                            .foregroundStyle(.white)
                    } else if item.uploadedID == nil {
                        Color.black.opacity(0.3)
                        if let progress = item.progress {
                            ProgressView(value: progress).progressViewStyle(.circular).tint(.white)
                        } else {
                            ProgressView().tint(.white)
                        }
                    }
                }
                .frame(width: 64, height: 64)
                .clipShape(.rect(cornerRadius: 12, style: .continuous))
                Text(item.name)
                    .font(.axCaption2)
                    .foregroundStyle(failed ? Theme.destructive : Theme.muted)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .frame(width: 64)
            }
            Button("Remove", systemImage: "xmark.circle.fill") { model.tray.remove(item.id) }
                .labelStyle(.iconOnly)
                .foregroundStyle(.white, Theme.muted)
                .buttonStyle(.plain)
                .offset(x: 6, y: -6)
        }
        .help(failure ?? item.name)
        .accessibilityIdentifier(AccessibilityID.Chats.attachment)
        .task(id: item.id) {
            guard item.isImage else { return }
            let file = item.file
            thumbnail = await Task.detached {
                (try? Data(contentsOf: file)).flatMap { ImageDecoder.decode($0, maxPixel: 160) }
            }.value
        }
    }

    private var failed: Bool { failure != nil }

    private var failure: String? {
        if case .failed(let reason) = item.phase { return reason }
        return nil
    }
}
