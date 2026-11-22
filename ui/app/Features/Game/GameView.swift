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
                    VStack(spacing: 12) {
                        PlayerCard(model: model, seat: 1 - model.perspective)
                        GameBoard(
                            session: session,
                            perspective: model.perspective,
                            interactive: model.interactive,
                            onCell: { cell in Task { await model.tap(cell: cell) } },
                            onMove: { move in Task { await model.play(move) } }
                        )
                        .padding(8)
                        .cardBackground()
                        .opacity(model.pending ? 0.7 : 1)
                        PlayerCard(model: model, seat: model.perspective)
                    }
                } panel: { wide in
                    StatusCard(model: model)
                    if session.status == .waiting {
                        InviteCard(id: session.id)
                    }
                    if !model.moves.isEmpty {
                        MovesCard(moves: model.moves, highlightedPly: model.moves.count, layout: MoveListLayout(session.kind), wide: wide)
                    }
                }
                .bottomActions {
                    GameActions(model: model, confirmsResign: $confirmsResign, onReplay: onReplay)
                }
                .toolbar {
                    if session.status != .waiting {
                        ToolbarItem(placement: .primaryAction) {
                            ShareLink(item: session.id) {
                                Label("Share game ID", systemImage: "square.and.arrow.up")
                            }
                        }
                    }
                }
            } else if let error = model.loadError {
                ContentUnavailableView {
                    Label("Game unavailable", systemImage: "exclamationmark.triangle")
                } description: {
                    Text(error)
                } actions: {
                    Button("Try again") { Task { await model.retry() } }
                        .buttonStyle(.glassProminent)
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
        .onChange(of: model.left) { _, left in
            if left { dismiss() }
        }
        .task { await model.start() }
        .onDisappear { model.stop() }
    }
}

private struct PlayerCard: View {
    let model: GameViewModel
    let seat: Int

    private var onMove: Bool { model.clockRunning(seat: seat) }

    var body: some View {
        HStack(spacing: 12) {
            SeatMarker(kind: model.kind, seat: seat)
            VStack(alignment: .leading, spacing: 2) {
                Text(model.name(seat: seat))
                    .font(.axHeadline)
                    .lineLimit(1)
                Text(model.kind.seatNames[seat])
                    .font(.axCaption)
                    .foregroundStyle(Theme.muted)
            }
            Spacer(minLength: 8)
            if let remaining = model.remaining(seat: seat) {
                GameClockView(remaining: remaining, running: onMove, sampledAt: model.receivedAt)
                    .accessibilityIdentifier(AccessibilityID.Game.clock(seat))
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .cardBackground()
        .overlay {
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .strokeBorder(onMove ? Color.accentColor.opacity(0.75) : .clear, lineWidth: 1.5)
        }
        .animation(.snappy, value: onMove)
    }
}

private struct SeatMarker: View {
    @Environment(\.colorScheme) private var colorScheme
    let kind: GameKind
    let seat: Int

    var body: some View {
        Group {
            if kind == .ticTacToe {
                let ink = SeatStyle.mark(seat: seat, palette: .resolve(colorScheme))
                Circle()
                    .fill(ink.opacity(0.14))
                    .overlay {
                        Text(kind.seatNames[seat])
                            .font(.system(size: 16, weight: .heavy, design: .rounded))
                            .foregroundStyle(ink)
                    }
            } else {
                Circle()
                    .fill(SeatStyle.color(kind, seat: seat).gradient)
                    .overlay {
                        Circle().strokeBorder(.separator, lineWidth: 0.5)
                    }
            }
        }
        .frame(width: 34, height: 34)
        .accessibilityHidden(true)
    }
}

enum SeatStyle {
    static func color(_ kind: GameKind, seat: Int) -> Color {
        switch kind {
        case .quoridor: seat == 0 ? .purple : .green
        case .chess: Color(white: seat == 0 ? 0.92 : 0.15)
        case .checkers: Color(white: seat == 0 ? 0.15 : 0.92)
        case .durak: seat == 0 ? .indigo : .teal
        case .ticTacToe: seat == 0 ? .blue : .orange
        }
    }

    static func mark(seat: Int, palette: BoardPalette) -> Color {
        seat == 0 ? palette.violet : .primary
    }
}

private struct StatusCard: View {
    let model: GameViewModel

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Text(model.statusText)
                    .font(.geist(18, .semibold, relativeTo: .title3))
                    .accessibilityIdentifier(AccessibilityID.Game.status)
                Spacer(minLength: 8)
                if model.isSpectator {
                    Label("Watching", systemImage: "eye.fill")
                        .font(.geist(12, .semibold, relativeTo: .caption))
                        .foregroundStyle(.tint)
                }
            }
            if let detail = model.detailText {
                Text(detail)
                    .foregroundStyle(Theme.muted)
            }
            if model.connection == .closed, !model.isFinished {
                HStack {
                    Label("Disconnected", systemImage: "wifi.slash")
                    Spacer()
                    Button("Reconnect") { Task { await model.retry() } }
                        .buttonStyle(.glass)
                }
                .font(.axCallout)
                .foregroundStyle(Theme.muted)
            } else if !model.connected, !model.isFinished {
                Label("Connecting…", systemImage: "wifi.exclamationmark")
                    .font(.axCallout)
                    .foregroundStyle(Theme.muted)
            }
            if model.spectators > 0 {
                Label("\(model.spectators) watching", systemImage: "eye")
                    .font(.axCallout)
                    .foregroundStyle(Theme.muted)
                    .accessibilityIdentifier(AccessibilityID.Game.spectators)
            }
            if let error = model.error {
                HStack {
                    Label(error, systemImage: "exclamationmark.circle.fill")
                        .foregroundStyle(.red)
                        .accessibilityIdentifier(AccessibilityID.Game.error)
                    Spacer()
                    Button("Retry") { Task { await model.retry() } }
                        .buttonStyle(.glass)
                }
                .font(.axCallout)
            }
            if model.drawOfferedToMe {
                VStack(alignment: .leading, spacing: 10) {
                    Text("Your opponent offers a draw")
                        .font(.axHeadline)
                    HStack(spacing: 10) {
                        Button("Accept") { Task { await model.draw(.accept) } }
                            .buttonStyle(.glassProminent)
                            .accessibilityIdentifier(AccessibilityID.Game.acceptDraw)
                        Button("Decline") { Task { await model.draw(.decline) } }
                            .buttonStyle(.glass)
                            .accessibilityIdentifier(AccessibilityID.Game.declineDraw)
                    }
                }
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color.accentColor.opacity(0.12), in: .rect(cornerRadius: 16, style: .continuous))
            } else if model.drawOfferedByMe {
                Label("Draw offered", systemImage: "equal.circle")
                    .foregroundStyle(Theme.muted)
            }
            if model.canJoin {
                Button {
                    Task { await model.join() }
                } label: {
                    Text("Join game")
                        .font(.axHeadline)
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.glassProminent)
                .controlSize(.large)
            }
        }
        .card()
        .animation(.snappy, value: model.statusText)
    }
}

private struct InviteCard: View {
    let id: String

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Invite a friend")
                .font(.axHeadline)
            HStack(spacing: 12) {
                Text(id)
                    .font(.axFootnote.monospaced())
                    .foregroundStyle(Theme.muted)
                    .textSelection(.enabled)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .accessibilityIdentifier(AccessibilityID.Game.sessionID)
                Spacer(minLength: 0)
                ShareLink(item: id) {
                    Label("Share", systemImage: "square.and.arrow.up")
                        .font(.geist(14, .semibold, relativeTo: .subheadline))
                }
                .buttonStyle(.glassProminent)
            }
        }
        .card()
    }
}

struct MovesCard: View {
    let moves: [String]
    let highlightedPly: Int?
    let layout: MoveListLayout
    let wide: Bool

    var body: some View {
        if wide {
            VStack(alignment: .leading, spacing: 4) {
                Text("Moves")
                    .font(.axHeadline)
                    .padding([.top, .horizontal], 16)
                MoveListView(moves: moves, highlightedPly: highlightedPly, layout: layout)
                    .frame(maxHeight: 360)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .cardBackground()
        } else {
            MoveStrip(moves: moves, highlightedPly: highlightedPly, layout: layout)
        }
    }
}

private struct GameActions: View {
    let model: GameViewModel
    @Binding var confirmsResign: Bool
    let onReplay: () -> Void

    var body: some View {
        if model.canOfferDraw {
            Button("Offer draw", systemImage: "equal.circle") { Task { await model.draw(.offer) } }
                .disabled(!model.connected || model.pending)
                .accessibilityIdentifier(AccessibilityID.Game.offerDraw)
        }
        if model.canUndo {
            Button("Undo", systemImage: "arrow.uturn.backward") { Task { await model.undo() } }
                .accessibilityIdentifier(AccessibilityID.Game.undo)
        }
        Spacer()
        if model.canResign {
            let waiting = model.session?.status == .waiting
            Button(waiting ? "Cancel game" : "Resign", systemImage: waiting ? "xmark" : "flag.fill", role: .destructive) {
                confirmsResign = true
            }
            .accessibilityIdentifier(AccessibilityID.Game.resign)
            .confirmationDialog(model.resignPrompt, isPresented: $confirmsResign, titleVisibility: .visible) {
                Button(waiting ? "Cancel game" : "Resign", role: .destructive) {
                    Task { await model.resign() }
                }
            }
        }
        if model.isFinished {
            Button("Replay", systemImage: "play.fill", action: onReplay)
                .buttonStyle(.glassProminent)
                .accessibilityIdentifier(AccessibilityID.Game.replay)
        }
    }
}
