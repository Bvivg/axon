import SwiftUI

struct PracticeView<Game: PracticeGame>: View {
    @State private var model = PracticeViewModel<Game>()

    var body: some View {
        AdaptiveGameLayout {
            GameBoard(
                session: model.session,
                perspective: model.perspective,
                interactive: model.interactive,
                onCell: { model.play("\($0)") },
                onMove: model.play
            )
            .padding(8)
            .cardBackground()
            .overlay {
                if model.outcome != nil, !model.dismissed {
                    PracticeResult(model: model)
                }
            }
        } panel: { wide in
            VStack(alignment: .leading, spacing: 12) {
                HStack(alignment: .firstTextBaseline) {
                    Text(model.status)
                        .font(.geist(17, .semibold, relativeTo: .headline))
                        .foregroundStyle(Theme.foreground)
                        .contentTransition(.opacity)
                        .accessibilityIdentifier(AccessibilityID.Practice.status)
                    Spacer(minLength: 8)
                    if model.thinking {
                        ProgressView().controlSize(.small)
                    }
                }
                PracticeSetup(model: model)
            }
            .card()
            if !model.moves.isEmpty {
                MovesCard(moves: model.moves, highlightedPly: model.moves.count, layout: MoveListLayout(Game.kind), wide: wide)
            }
        }
        .bottomActions {
            Button("Undo", systemImage: "arrow.uturn.backward", action: model.undo)
                .disabled(!model.canUndo)
                .accessibilityIdentifier(AccessibilityID.Practice.undo)
            Spacer()
            Button("New game", systemImage: "arrow.clockwise") { model.restart() }
                .accessibilityIdentifier(AccessibilityID.Practice.restart)
        }
        .navigationTitle("\(Game.kind.title) practice")
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .onAppear(perform: model.start)
        .onDisappear(perform: model.stop)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier(AccessibilityID.Practice.root)
    }
}

private struct PracticeSetup<Game: PracticeGame>: View {
    let model: PracticeViewModel<Game>

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            if Game.opponents.count > 1 {
                Picker("Opponent", selection: Binding(get: { model.opponent }, set: { model.restart(opponent: $0) })) {
                    ForEach(Game.opponents, id: \.self) { Text($0.title).tag($0) }
                }
                .pickerStyle(.segmented)
                .accessibilityIdentifier(AccessibilityID.Practice.opponent)
            }
            if Game.choosesSide, !model.isLocal {
                Picker("Side", selection: Binding(get: { model.seat }, set: { model.restart(seat: $0) })) {
                    ForEach(0..<2, id: \.self) { seat in
                        Text("Play \(Game.kind.seatNames[seat].lowercased())").tag(seat)
                    }
                }
                .pickerStyle(.segmented)
                .accessibilityIdentifier(AccessibilityID.Practice.side)
            }
        }
    }
}

private struct PracticeResult<Game: PracticeGame>: View {
    let model: PracticeViewModel<Game>

    var body: some View {
        VStack(spacing: 12) {
            Text(model.title ?? "")
                .font(.axTitle2)
                .foregroundStyle(Theme.foreground)
            if let detail = Game.ending(model.position) {
                Text(detail.prefix(1).uppercased() + detail.dropFirst())
                    .font(.axSubheadline)
                    .foregroundStyle(Theme.muted)
            }
            HStack(spacing: 10) {
                Button("Close") { model.dismissed = true }
                    .buttonStyle(.bordered)
                Button("Play again") { model.restart() }
                    .buttonStyle(.borderedProminent)
                    .accessibilityIdentifier(AccessibilityID.Practice.again)
            }
            .controlSize(.large)
        }
        .padding(20)
        .background(.regularMaterial, in: .rect(cornerRadius: 18, style: .continuous))
        .shadow(color: .black.opacity(0.15), radius: 12, y: 4)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier(AccessibilityID.Practice.result)
    }
}

struct PracticeScreen: View {
    let game: GameKind

    var body: some View {
        switch game {
        case .ticTacToe: PracticeView<TicTacToePractice>()
        case .chess: PracticeView<ChessPractice>()
        case .quoridor: PracticeView<QuoridorPractice>()
        case .checkers: PracticeView<CheckersPractice>()
        case .durak: PracticeView<DurakPractice>()
        }
    }
}
