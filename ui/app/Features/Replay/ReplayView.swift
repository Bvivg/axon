import SwiftUI

struct ReplayView: View {
    @State private var model: ReplayViewModel

    init(sessionID: String, services: Services) {
        _model = State(initialValue: ReplayViewModel(sessionID: sessionID, game: services.game))
    }

    var body: some View {
        Group {
            if let frame = model.frame {
                AdaptiveGameLayout {
                    GameBoard(kind: model.kind, frame: frame)
                } panel: {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(model.positionText)
                            .font(.title2.weight(.bold))
                            .monospacedDigit()
                            .accessibilityIdentifier(AccessibilityID.Replay.position)
                        if let result = model.resultText {
                            Text(result)
                                .foregroundStyle(.secondary)
                                .accessibilityIdentifier(AccessibilityID.Replay.result)
                        }
                    }
                    if model.lastIndex > 0 {
                        Slider(value: model.sliderValue, in: 0...Double(model.lastIndex), step: 1) {
                            Text("Move")
                        }
                        .accessibilityIdentifier(AccessibilityID.Replay.slider)
                    }
                    ReplayControls(model: model)
                    if !model.moves.isEmpty {
                        MoveListView(moves: model.moves, highlightedPly: model.highlightedPly)
                    }
                }
            } else if let error = model.error {
                ContentUnavailableView {
                    Label("Replay unavailable", systemImage: "film")
                } description: {
                    Text(error)
                } actions: {
                    Button("Try again") { Task { await model.load() } }
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationTitle("Replay")
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
        .task { await model.load() }
    }
}

private struct ReplayControls: View {
    let model: ReplayViewModel

    var body: some View {
        HStack(spacing: 12) {
            Button("First", systemImage: "backward.end.fill", action: model.first)
                .disabled(model.atStart)
                .keyboardShortcut(.home, modifiers: [])
                .accessibilityIdentifier(AccessibilityID.Replay.first)
            Button("Previous", systemImage: "backward.fill", action: model.previous)
                .disabled(model.atStart)
                .keyboardShortcut(.leftArrow, modifiers: [])
                .accessibilityIdentifier(AccessibilityID.Replay.previous)
            Button("Next", systemImage: "forward.fill", action: model.next)
                .disabled(model.atEnd)
                .keyboardShortcut(.rightArrow, modifiers: [])
                .accessibilityIdentifier(AccessibilityID.Replay.next)
            Button("Last", systemImage: "forward.end.fill", action: model.last)
                .disabled(model.atEnd)
                .keyboardShortcut(.end, modifiers: [])
                .accessibilityIdentifier(AccessibilityID.Replay.last)
        }
        .labelStyle(.iconOnly)
        .buttonStyle(.bordered)
        .controlSize(.large)
    }
}
