import Foundation

extension ConversationViewModel {
    func startEdit(_ message: ChatMessage) {
        guard isMine(message), message.isEditable else { return }
        replyTo = nil
        editing = message
        draft = message.body
    }

    func cancelEdit() {
        editing = nil
        draft = ""
    }

    func saveEdit(_ message: ChatMessage) async {
        let body = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard canSend else { return }
        let client = Self.clientID()
        pendingEdits[client] = message.id
        if await hub.send(.edit(clientID: client, message: message.id, body: body)) {
            editing = nil
            draft = ""
        } else {
            pendingEdits[client] = nil
            notice = "You're offline. Your change was not saved."
        }
    }

    func send() async {
        if let editing {
            await saveEdit(editing)
            return
        }
        let body = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard canSend, let target else { return }
        guard body.count <= Self.maxLength else {
            notice = "A message can contain at most \(Self.maxLength) characters."
            return
        }
        let restore = (draft, replyTo)
        let attached = tray.take()
        let outgoing = OutgoingDraft(
            clientID: Self.clientID(), target: target, body: body,
            uploadIDs: attached.compactMap(\.uploadedID), replyTo: replyTo?.id
        )
        outbox.add(outgoing, quote: replyTo.map(Self.quote))
        draft = ""
        replyTo = nil
        guard await hub.send(.send(outgoing)) else {
            outbox.remove(outgoing.clientID)
            (draft, replyTo) = restore
            tray.restore(attached)
            notice = "You're offline. The message was not sent."
            return
        }
        outbox.arm(outgoing.clientID)
    }

    func sendVoice(_ file: URL) {
        let reply = replyTo
        replyTo = nil
        sendingVoice += 1
        Task {
            defer { sendingVoice -= 1 }
            do throws(APIError) {
                let uploaded = try await services.media.upload(file, name: "voice.m4a", as: .voice) { _ in }
                guard let target else { return }
                let outgoing = OutgoingDraft(
                    clientID: Self.clientID(), target: target, body: "", uploadIDs: [uploaded.id], replyTo: reply?.id
                )
                outbox.add(outgoing, quote: reply.map(Self.quote))
                if await hub.send(.send(outgoing)) {
                    outbox.arm(outgoing.clientID)
                } else {
                    outbox.unconfirm(outgoing.clientID)
                }
            } catch {
                if error.code != .cancelled { notice = error.message.isEmpty ? "The voice message was not sent." : error.message }
            }
        }
    }

    func isRead(_ message: ChatMessage) -> Bool {
        readSeqs.values.contains { $0 >= message.seq }
    }

    func typingText(in room: String) -> String? {
        let others = hub.typing.typing(in: room).filter { $0 != me }
        guard !others.isEmpty else { return nil }
        return isDirect ? "typing…" : ChatFormat.typing(others.map(name(of:)))
    }

    func message(id: String) -> ChatMessage? { timeline.message(id: id) }

    var selectedMessages: [ChatMessage] {
        guard let selection else { return [] }
        return timeline.messages.filter { selection.contains($0.id) }
    }

    var canDeleteSelection: Bool {
        let picked = selectedMessages
        return !picked.isEmpty && picked.allSatisfy(isMine)
    }

    func retry(_ item: OutgoingMessage) async {
        guard let draft = outbox.resend(item.id) else { return }
        if await hub.send(.send(draft)) {
            outbox.arm(item.id)
        } else {
            outbox.unconfirm(item.id)
        }
    }

    func dismiss(_ item: OutgoingMessage) {
        outbox.remove(item.id)
    }

    func reply(to message: ChatMessage) {
        guard !message.isDeleted, !message.isSystem else { return }
        if editing != nil { cancelEdit() }
        replyTo = message
    }

    func attach(_ files: [URL], as purpose: UploadPurpose) {
        if !tray.attach(files, as: purpose) {
            notice = "You can attach up to \(AttachmentTray.limit) files to one message."
        }
    }

    func delete(_ ids: [String]) async {
        let own = ids.filter { id in timeline.message(id: id).map(isMine) ?? false }
        guard !own.isEmpty else { return }
        if await hub.send(.delete(own)) {
            selection = nil
        } else {
            notice = "You're offline. Nothing was deleted."
        }
    }

    func toggle(_ message: ChatMessage) {
        guard message.canSelect else { return }
        var picked = selection ?? []
        if picked.contains(message.id) { picked.remove(message.id) } else { picked.insert(message.id) }
        selection = picked
    }

    func forward(_ messages: [String], to rooms: [String], comment: String) async -> Bool {
        do throws(APIError) {
            _ = try await services.chat.forward(messages, to: rooms, comment: comment, clientID: Self.clientID())
            selection = nil
            notice = rooms.count == 1 ? "Forwarded." : "Forwarded to \(rooms.count) chats."
            return true
        } catch {
            notice = Self.message(error)
            return false
        }
    }

    func readers(of message: ChatMessage) async -> [MessageReader] {
        guard let readers = try? await services.chat.readers(of: message.id) else { return [] }
        await hub.ensureProfiles(readers.map(\.userID))
        return readers.filter { $0.userID != me }
    }

    func search(_ query: String) async {
        let needle = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let roomID, needle.count >= 2 else {
            searchResults = []
            return
        }
        searching = true
        defer { searching = false }
        searchResults = (try? await services.chat.search(in: roomID, for: needle)) ?? []
        await hub.ensureProfiles(searchResults.map(\.authorID))
    }

    func jump(to message: ChatMessage) async {
        var pages = 0
        while timeline.message(id: message.id) == nil, hasOlder, pages < Self.jumpPages {
            await loadOlder()
            pages += 1
        }
        guard timeline.message(id: message.id) != nil else {
            notice = "That message is too far back."
            return
        }
        highlighted = message.id
        scrollTarget = message.id
        Task { [weak self] in
            try? await Task.sleep(for: .seconds(2))
            if self?.highlighted == message.id { self?.highlighted = nil }
        }
    }

    nonisolated static func clientID() -> String {
        UUID().uuidString.lowercased()
    }

    nonisolated static func quote(_ message: ChatMessage) -> Axon_Chat_V1_ReplyPreview {
        Axon_Chat_V1_ReplyPreview.with {
            $0.id = message.id
            $0.authorID = message.authorID
            $0.kind = message.kind
            $0.body = message.body
            $0.deleted = message.isDeleted
        }
    }
}
