import SwiftUI

extension EnvironmentValues {
    @Entry var calls: CallEngine?
}

struct CallPresenter: ViewModifier {
    @Bindable var engine: CallEngine
    let hub: ChatsViewModel

    func body(content: Content) -> some View {
        content
            .safeAreaInset(edge: .top, spacing: 0) {
                if engine.call?.isOpen == true, engine.minimized {
                    CallBanner(engine: engine, hub: hub)
                }
            }
        #if os(iOS)
            .fullScreenCover(isPresented: presented) { screen }
        #else
            .sheet(isPresented: presented) {
                screen.frame(minWidth: 420, minHeight: 620)
            }
        #endif
            .alert("Could not start the call", isPresented: failed) {
                if engine.canRetry {
                    Button("Try again") { Task { await engine.retry() } }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(engine.error ?? "")
            }
            .onChange(of: engine.call?.state) { _, state in
                if state == .ended, engine.minimized { engine.dismiss() }
            }
            .environment(\.calls, engine)
    }

    private var failed: Binding<Bool> {
        Binding(
            get: { engine.call == nil && engine.error != nil },
            set: { shown in
                guard !shown, engine.call == nil else { return }
                engine.error = nil
            }
        )
    }

    private var screen: some View {
        CallScreen(engine: engine, hub: hub)
    }

    private var presented: Binding<Bool> {
        Binding(
            get: { engine.call != nil && !engine.minimized },
            set: { shown in
                guard !shown else { return }
                if engine.call?.isOpen == true {
                    engine.minimized = true
                } else {
                    engine.dismiss()
                }
            }
        )
    }
}

private struct CallBanner: View {
    let engine: CallEngine
    let hub: ChatsViewModel

    var body: some View {
        Button {
            engine.minimized = false
        } label: {
            HStack(spacing: 10) {
                Image(systemName: engine.call?.isVideo == true ? "video.fill" : "phone.fill")
                    .font(.system(size: 13, weight: .semibold))
                Text(CallScreen.title(engine.call, engine: engine))
                    .font(.geist(14, .semibold, relativeTo: .subheadline))
                    .lineLimit(1)
                Spacer(minLength: 8)
                CallStatusText(engine: engine, tint: .white)
                Image(systemName: "chevron.up")
                    .font(.system(size: 12, weight: .bold))
            }
            .foregroundStyle(.white)
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .background(Theme.success, in: Capsule())
            .padding(.horizontal, 12)
            .padding(.vertical, 4)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Return to call")
        .accessibilityIdentifier(AccessibilityID.Calls.banner)
    }
}

struct CallMenu: View {
    let target: CallTarget?
    let name: String
    var group = false
    @Environment(\.calls) private var engine

    var body: some View {
        if let engine, !group || engine.capabilities.sfu {
            Menu {
                Button("Audio call", systemImage: "phone") { start(engine, .audio) }
                    .accessibilityIdentifier(AccessibilityID.Calls.startAudio)
                Button("Video call", systemImage: "video") { start(engine, .video) }
                    .accessibilityIdentifier(AccessibilityID.Calls.startVideo)
            } label: {
                Label(name.isEmpty ? "Call" : "Call \(name)", systemImage: "phone")
            }
            .disabled(target == nil || !engine.available || engine.pending || engine.call?.isOpen == true)
            .accessibilityIdentifier(AccessibilityID.Calls.start)
        }
    }

    private func start(_ engine: CallEngine, _ mode: CallMode) {
        guard let target else { return }
        Task { await engine.start(target, mode: mode, name: name) }
    }
}
