import SwiftUI
@preconcurrency import WebRTC
#if os(iOS)
import AVKit
#endif

struct CallScreen: View {
    @Bindable var engine: CallEngine
    let hub: ChatsViewModel
    @State private var adding = false
    @State private var picked: [PublicProfile] = []

    var body: some View {
        ZStack {
            Color(light: 0x161A26, dark: 0x0B0E16).ignoresSafeArea()
            stage.ignoresSafeArea()
            VStack(spacing: 0) {
                header
                Spacer()
                if let error = engine.error { notice(error) }
                controls
            }
            .padding(.horizontal, 20)
            .padding(.bottom, 24)
        }
        .environment(\.colorScheme, .dark)
        .sheet(isPresented: $adding) {
            NavigationStack {
                PeoplePicker(hub: hub, excluded: Set(call?.participants.map(\.userID) ?? []), picked: $picked) { person in
                    adding = false
                    Task { await engine.invite(person.id) }
                }
                .navigationTitle("Add to call")
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Close", systemImage: "xmark") { adding = false }
                    }
                }
            }
            .presentationBackground(Theme.popover)
        }
        .task(id: engine.call?.state) {
            guard engine.call?.state == .ended, engine.error == nil else { return }
            try? await Task.sleep(for: .seconds(2))
            if engine.call?.state == .ended, engine.error == nil { engine.dismiss() }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier(AccessibilityID.Calls.screen)
    }

    private var call: Call? { engine.call }

    private var title: String { Self.title(call, engine: engine) }

    static func title(_ call: Call?, engine: CallEngine) -> String {
        guard let call else { return "Call" }
        if call.isGroup { return "Group call" }
        guard let peer = call.peer(for: engine.me) else { return "Call" }
        return engine.names(peer)
    }

    private var avatar: URL? {
        guard let call, !call.isGroup, let peer = call.peer(for: engine.me) else { return nil }
        return hub.people[peer]?.avatarLink
    }

    @ViewBuilder private var stage: some View {
        let tiles = engine.media?.tiles ?? []
        ZStack(alignment: .topTrailing) {
            if tiles.isEmpty {
                VStack(spacing: 18) {
                    Spacer()
                    Avatar(name: title, url: avatar, size: 112, cornerRadius: 56)
                    Spacer()
                    Spacer()
                }
                .frame(maxWidth: .infinity)
            } else if tiles.count == 1, let track = tiles[0].track {
                CallVideoView(track: track)
                #if os(iOS)
                    .background { CallPictureInPicture(track: track as? RTCVideoTrack) }
                #endif
            } else {
                CallGrid(tiles: tiles, names: engine.names)
            }
            if let preview = engine.media?.preview {
                CallVideoView(track: preview, mirrored: true)
                    .frame(width: 112, height: 160)
                    .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).stroke(.white.opacity(0.18)))
                    .padding(.top, 110)
                    .padding(.trailing, 16)
            }
        }
        .id(engine.revision)
    }

    private var header: some View {
        VStack(spacing: 6) {
            HStack {
                if engine.isOngoing, !engine.isIncoming {
                    Button {
                        engine.minimized = true
                    } label: {
                        Image(systemName: "chevron.down")
                            .font(.system(size: 17, weight: .semibold))
                            .frame(width: 44, height: 44)
                    }
                    .buttonStyle(.glass)
                    .accessibilityLabel("Minimize call")
                    .accessibilityIdentifier(AccessibilityID.Calls.minimize)
                }
                Spacer()
            }
            Text(title)
                .font(.axTitle2)
                .foregroundStyle(.white)
                .lineLimit(1)
            CallStatusText(engine: engine)
        }
        .padding(.top, 8)
    }

    private func notice(_ text: String) -> some View {
        VStack(spacing: 10) {
            Text(text)
                .font(.axFootnote)
                .foregroundStyle(.white)
                .multilineTextAlignment(.center)
            if engine.canRetry {
                Button("Try again") { Task { await engine.retry() } }
                    .buttonStyle(.glass)
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity)
        .background(Theme.destructive.opacity(0.35), in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .padding(.bottom, 16)
    }

    @ViewBuilder private var controls: some View {
        if let call, call.state == .ended {
            endedControls(call)
        } else if engine.isIncoming {
            incomingControls
        } else {
            CallControls(engine: engine, adding: $adding)
        }
    }

    private var incomingControls: some View {
        HStack(spacing: 28) {
            CallButton(symbol: "phone.down.fill", label: "Decline", tint: Theme.destructive) {
                Task { await engine.end() }
            }
            .accessibilityIdentifier(AccessibilityID.Calls.decline)
            if call?.isVideo == true {
                CallButton(symbol: "phone.fill", label: "Audio", tint: Theme.success) {
                    Task { await engine.accept(audioOnly: true) }
                }
                .accessibilityIdentifier(AccessibilityID.Calls.acceptAudio)
            }
            CallButton(symbol: call?.isVideo == true ? "video.fill" : "phone.fill", label: "Accept", tint: Theme.success) {
                Task { await engine.accept() }
            }
            .accessibilityIdentifier(AccessibilityID.Calls.accept)
        }
        .disabled(engine.pending)
    }

    private func endedControls(_ call: Call) -> some View {
        HStack(spacing: 28) {
            if !call.isGroup, let peer = call.peer(for: engine.me) {
                CallButton(symbol: "phone.fill", label: "Call back", tint: Theme.success) {
                    Task { await engine.start(.person(peer), mode: call.mode, name: engine.names(peer)) }
                }
                .accessibilityIdentifier(AccessibilityID.Calls.callBack)
            }
            CallButton(symbol: "xmark", label: "Close", tint: .white.opacity(0.2)) {
                engine.dismiss()
            }
            .accessibilityIdentifier(AccessibilityID.Calls.close)
        }
    }
}

struct CallStatusText: View {
    let engine: CallEngine
    var tint: Color = .white.opacity(0.7)

    var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { context in
            Text(text(at: context.date))
                .font(.axMono(14, relativeTo: .subheadline))
                .foregroundStyle(tint)
                .accessibilityIdentifier(AccessibilityID.Calls.status)
        }
    }

    private func text(at now: Date) -> String {
        guard let call = engine.call else { return "" }
        if engine.ending { return "Ending…" }
        if call.state == .ended { return call.outcome(for: engine.me) }
        if engine.disconnected || engine.reconnecting { return "Reconnecting…" }
        switch call.state {
        case .ringing:
            if engine.isIncoming { return call.isVideo ? "Incoming video call" : "Incoming call" }
            return "Ringing…"
        case .connecting:
            return engine.isIncoming ? "Incoming call" : "Connecting…"
        case .active:
            guard let since = call.connectedSince else { return "Connected" }
            return Clock.duration(.seconds(max(0, Int(now.timeIntervalSince(since)))))
        default:
            return ""
        }
    }
}

struct CallControls: View {
    let engine: CallEngine
    @Binding var adding: Bool

    var body: some View {
        let media = engine.media
        let muted = media?.muted == true
        let cameraOff = media?.cameraOff != false
        HStack(spacing: 14) {
            CallButton(symbol: muted ? "mic.slash.fill" : "mic.fill", label: muted ? "Unmute" : "Mute", active: muted) {
                engine.toggleMute()
            }
            .disabled(media == nil)
            .accessibilityIdentifier(AccessibilityID.Calls.mute)
            if engine.call?.isVideo == true {
                CallButton(symbol: cameraOff ? "video.slash.fill" : "video.fill", label: "Camera", active: cameraOff && media != nil) {
                    Task { await engine.toggleCamera() }
                }
                .disabled(media == nil)
                .accessibilityIdentifier(AccessibilityID.Calls.camera)
                if !cameraOff {
                    CallButton(symbol: "arrow.triangle.2.circlepath.camera", label: "Flip") { engine.switchCamera() }
                }
            }
            #if os(iOS)
            RouteButton()
            #endif
            if canAdd {
                CallButton(symbol: "person.badge.plus", label: "Add") { adding = true }
                    .accessibilityIdentifier(AccessibilityID.Calls.add)
            }
            CallButton(symbol: "phone.down.fill", label: engine.call?.isGroup == true ? "Leave" : "End", tint: Theme.destructive) {
                Task { await engine.hangUp() }
            }
            .accessibilityIdentifier(AccessibilityID.Calls.end)
        }
    }

    private var canAdd: Bool {
        guard let call = engine.call, call.state == .active, engine.capabilities.sfu else { return false }
        guard call.isGroup else { return true }
        return call.hasOwnerID && call.ownerID == engine.me && call.participants.count < engine.capabilities.maxParticipants
    }
}

struct CallButton: View {
    let symbol: String
    let label: String
    var tint: Color = .white.opacity(0.16)
    var active = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 6) {
                Image(systemName: symbol)
                    .font(.system(size: 20, weight: .semibold))
                    .foregroundStyle(active ? Color.black : Color.white)
                    .frame(width: 58, height: 58)
                    .background(active ? Color.white : tint, in: Circle())
                Text(label)
                    .font(.axCaption)
                    .foregroundStyle(.white.opacity(0.8))
            }
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }
}

#if os(iOS)
private struct RouteButton: View {
    var body: some View {
        VStack(spacing: 6) {
            RoutePicker()
                .frame(width: 58, height: 58)
                .background(Color.white.opacity(0.16), in: Circle())
            Text("Audio")
                .font(.axCaption)
                .foregroundStyle(.white.opacity(0.8))
        }
        .accessibilityLabel("Audio output")
    }
}

private struct RoutePicker: UIViewRepresentable {
    func makeUIView(context: Context) -> AVRoutePickerView {
        let view = AVRoutePickerView()
        view.tintColor = .white
        view.activeTintColor = .white
        view.prioritizesVideoDevices = false
        return view
    }

    func updateUIView(_ view: AVRoutePickerView, context: Context) {}
}
#endif

private struct CallGrid: View {
    let tiles: [CallTile]
    let names: (String) -> String

    var body: some View {
        let columns = [GridItem(.flexible(), spacing: 6), GridItem(.flexible(), spacing: 6)]
        LazyVGrid(columns: columns, spacing: 6) {
            ForEach(tiles) { tile in
                ZStack(alignment: .bottomLeading) {
                    if let track = tile.track {
                        CallVideoView(track: track)
                    } else {
                        Color.white.opacity(0.08)
                    }
                    Text(names(tile.userID))
                        .font(.axCaption)
                        .foregroundStyle(.white)
                        .padding(6)
                }
                .aspectRatio(3 / 4, contentMode: .fill)
                .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
            }
        }
        .padding(.horizontal, 8)
        .padding(.top, 120)
    }
}
