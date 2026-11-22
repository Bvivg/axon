import SwiftUI

struct CallDetailView: View {
    let callID: String
    let model: CallsViewModel
    let hub: ChatsViewModel
    @Environment(\.calls) private var engine

    var body: some View {
        if let call = model.call(callID) {
            ProfileScroll(title: "Call") {
                header(call)
                ProfileSection(call.isGroup ? "Group calls" : "Calls with \(model.name(of: call))") {
                    let related = model.related(to: call)
                    VStack(spacing: 0) {
                        ForEach(related, id: \.id) { other in
                            CallHistoryLine(
                                call: other,
                                summary: model.summary(of: other),
                                direction: other.direction(for: model.me),
                                selected: other.id == callID
                            )
                            if other.id != related.last?.id {
                                Divider().overlay(Theme.line)
                            }
                        }
                    }
                }
            }
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier(AccessibilityID.Calls.detail)
        } else {
            ContentUnavailableView("Call not found", systemImage: "phone")
                .background(Theme.background)
        }
    }

    private func header(_ call: Call) -> some View {
        let name = model.name(of: call)
        let peer = model.peer(of: call)
        let canStart = engine?.canStart ?? false
        return VStack(spacing: 10) {
            Avatar(name: name, url: model.avatar(of: call), size: 84, cornerRadius: 42)
            Text(name)
                .font(.axTitle3)
                .foregroundStyle(Theme.foreground)
            if let handle = model.handle(of: call) {
                Text(handle).font(.axSubheadline).foregroundStyle(Theme.muted)
            }
            HStack(spacing: 10) {
                if let peer {
                    Button("Audio", systemImage: "phone.fill") { start(peer, .audio, name: name) }
                        .disabled(!canStart)
                        .accessibilityIdentifier(AccessibilityID.Calls.detailAudio)
                    Button("Video", systemImage: "video.fill") { start(peer, .video, name: name) }
                        .disabled(!canStart)
                        .accessibilityIdentifier(AccessibilityID.Calls.detailVideo)
                }
                if let chat = chatRoute(call, peer: peer) {
                    NavigationLink(value: chat) {
                        Label("Message", systemImage: "message.fill")
                    }
                    .accessibilityIdentifier(AccessibilityID.Calls.detailChat)
                }
            }
            .buttonStyle(.bordered)
            .controlSize(.regular)
            .padding(.top, 4)
        }
        .frame(maxWidth: .infinity)
        .surface(padding: 20)
    }

    private func chatRoute(_ call: Call, peer: String?) -> Route? {
        if let peer { return .person(peer) }
        return call.roomID.isEmpty ? nil : .room(call.roomID)
    }

    private func start(_ peer: String, _ mode: CallMode, name: String) {
        guard let engine else { return }
        let shown = hub.people[peer]?.shownName ?? name
        Task { await engine.start(.person(peer), mode: mode, name: shown) }
    }
}

private struct CallHistoryLine: View {
    let call: Call
    let summary: String
    let direction: CallDirection
    let selected: Bool

    var body: some View {
        let missed = direction == .missed
        HStack(spacing: 10) {
            Image(systemName: symbol)
                .font(.system(size: 13, weight: .semibold))
                .frame(width: 18)
            Text(summary)
                .font(.axSubheadline)
                .lineLimit(1)
            Spacer(minLength: 8)
            Text(Clock.short(call.createdAt.date))
                .font(.axMono(11, relativeTo: .caption2))
                .foregroundStyle(Theme.muted)
        }
        .foregroundStyle(missed ? Theme.destructive : Theme.foreground)
        .padding(.vertical, 10)
        .padding(.horizontal, 8)
        .background {
            if selected {
                RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Theme.primary.opacity(0.12))
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var symbol: String {
        if direction == .missed { return "phone.arrow.down.left.fill" }
        return call.isVideo ? "video" : "phone"
    }
}
