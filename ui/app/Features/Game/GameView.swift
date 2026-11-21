import SwiftUI

struct GameView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var model: GameViewModel
    @State private var confirmsResign = false
    let onReplay: () -> Void

    init(sessionID: String, services: Services, user: User, onReplay: @escaping () -> Void) {
        _model = State(initialValue: GameViewModel(sessionID: sessionID, me: user.id, services: services))
        self.onReplay = onReplay
    }

    var body: some View {
        Group {
            if let session = model.session {
                AdaptiveGameLayout {
                    VStack(spacing: 10) {
                        PlayerBar(model: model, seat: 1 - model.perspective)
                        GameBoard(
                            session: session,
                            perspective: model.perspective,
                            interactive: model.interactive,
                            onCell: { cell in Task { await model.tap(cell: cell) } },
                            onMove: { move in Task { await model.play(move) } }
                        )
                        .opacity(model.pending ? 0.7 : 1)
                        PlayerBar(model: model, seat: model.perspective)
                    }
                } panel: {
                    StatusPanel(model: model, confirmsResign: $confirmsResign, onReplay: onReplay)
                    if !model.moves.isEmpty {
                        MoveListView(moves: model.moves, highlightedPly: model.moves.count, layout: MoveListLayout(session.kind))
                    }
                    SessionIDRow(id: session.id, waiting: session.status == .waiting)
                }
            } else if let error = model.loadError {
                ContentUnavailableView {
                    Label("Game unavailable", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(error)
                } actions: {
                    Button("Try again") { Task { await model.retry() } }
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle(model.session?.kind.title ?? "Game")
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .confirmationDialog(model.resignPrompt, isPresented: $confirmsResign, titleVisibility: .visible) {
            Button(model.session?.status == .waiting ? "Cancel game" : "Resign", role: .destructive) {
                Task { await model.resign() }
            }
        }
        .onChange(of: model.left) { _, left in
            if left { dismiss() }
        }
        .task { await model.start() }
        .onDisappear { model.stop() }
    }
}

private struct PlayerBar: View {
    let model: GameViewModel
    let seat: Int

    var body: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(SeatStyle.color(model.kind, seat: seat))
                .frame(width: 10, height: 10)
            Text(model.name(seat: seat))
                .font(.subheadline.weight(.semibold))
                .lineLimit(1)
            Text(model.kind.seatNames[seat])
                .font(.caption)
                .foregroundStyle(.secondary)
            Spacer()
            if let remaining = model.remaining(seat: seat) {
                GameClockView(remaining: remaining, running: model.clockRunning(seat: seat), sampledAt: model.receivedAt)
                    .font(.body.monospacedDigit().weight(model.clockRunning(seat: seat) ? .bold : .regular))
                    .accessibilityIdentifier(AccessibilityID.Game.clock(seat))
            }
        }
        .padding(.horizontal, 4)
    }
}

enum SeatStyle {
    static func color(_ kind: GameKind, seat: Int) -> Color {
        switch kind {
        case .quoridor: seat == 0 ? .purple : .green
        case .chess: Color(white: seat == 0 ? 0.92 : 0.15)
        case .checkers: Color(white: seat == 0 ? 0.15 : 0.92)
        case .ticTacToe: seat == 0 ? .blue : .orange
        }
    }
}

private struct StatusPanel: View {
    let model: GameViewModel
    @Binding var confirmsResign: Bool
    let onReplay: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(model.statusText)
                    .font(.title2.weight(.bold))
                    .accessibilityIdentifier(AccessibilityID.Game.status)
                if let detail = model.detailText {
                    Text(detail)
                        .foregroundStyle(.secondary)
                }
                if model.connection == .closed, !model.isFinished {
                    HStack {
                        Label("Disconnected", systemImage: "wifi.slash")
                        Button("Reconnect") { Task { await model.retry() } }
                            .buttonStyle(.borderless)
                    }
                    .font(.caption)
                    .foregroundStyle(.secondary)
                } else if !model.connected, !model.isFinished {
                    Label("Connecting…", systemImage: "wifi.exclamationmark")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                if model.spectators > 0 {
                    Label("\(model.spectators) watching", systemImage: "eye")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .accessibilityIdentifier(AccessibilityID.Game.spectators)
                }
                if model.isSpectator {
                    Label("Watching", systemImage: "eye.fill")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.tint)
                }
            }
            if let error = model.error {
                HStack {
                    Text(error)
                        .foregroundStyle(.red)
                        .accessibilityIdentifier(AccessibilityID.Game.error)
                    Spacer()
                    Button("Retry") { Task { await model.retry() } }
                        .buttonStyle(.borderless)
                }
            }
            if model.drawOfferedToMe {
                HStack {
                    Text("Your opponent offers a draw")
                    Spacer()
                    Button("Accept") { Task { await model.draw(.accept) } }
                        .accessibilityIdentifier(AccessibilityID.Game.acceptDraw)
                    Button("Decline") { Task { await model.draw(.decline) } }
                        .accessibilityIdentifier(AccessibilityID.Game.declineDraw)
                }
                .buttonStyle(.bordered)
            } else if model.drawOfferedByMe {
                Text("Draw offered")
                    .foregroundStyle(.secondary)
            }
            ActionRow(model: model, confirmsResign: $confirmsResign, onReplay: onReplay)
        }
    }
}

private struct ActionRow: View {
    let model: GameViewModel
    @Binding var confirmsResign: Bool
    let onReplay: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            if model.canJoin {
                Button("Join game") { Task { await model.join() } }
                    .buttonStyle(.borderedProminent)
            }
            if model.canOfferDraw {
                Button("Offer draw") { Task { await model.draw(.offer) } }
                    .disabled(!model.connected || model.pending)
                    .accessibilityIdentifier(AccessibilityID.Game.offerDraw)
            }
            if model.canUndo {
                Button("Undo", systemImage: "arrow.uturn.backward") { Task { await model.undo() } }
                    .accessibilityIdentifier(AccessibilityID.Game.undo)
            }
            if model.canResign {
                Button(model.session?.status == .waiting ? "Cancel" : "Resign", role: .destructive) { confirmsResign = true }
                    .accessibilityIdentifier(AccessibilityID.Game.resign)
            }
            if model.isFinished {
                Button("Replay", systemImage: "play.circle", action: onReplay)
                    .buttonStyle(.borderedProminent)
                    .accessibilityIdentifier(AccessibilityID.Game.replay)
            }
        }
        .buttonStyle(.bordered)
    }
}

private struct SessionIDRow: View {
    let id: String
    let waiting: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(waiting ? "Share this ID with your opponent" : "Game ID")
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack {
                Text(id)
                    .font(.caption.monospaced())
                    .textSelection(.enabled)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .accessibilityIdentifier(AccessibilityID.Game.sessionID)
                Spacer()
                ShareLink(item: id) {
                    Label("Share", systemImage: "square.and.arrow.up")
                        .labelStyle(.iconOnly)
                }
            }
        }
    }
}
