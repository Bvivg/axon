import SwiftUI

enum AnalysisPalette {
    static let white = Color(light: 0xF4F1EA, dark: 0xE9E5DA)
    static let black = Color(light: 0x2B2E3D, dark: 0x0B0D14)
}

struct EvalBar: View {
    let position: PositionAnalysis?

    var body: some View {
        GeometryReader { proxy in
            VStack(spacing: 0) {
                Spacer(minLength: 0)
                AnalysisPalette.white
                    .frame(height: proxy.size.height * (position?.whiteShare ?? 0.5))
            }
            .animation(.easeOut(duration: 0.3), value: position?.whiteShare)
        }
        .frame(width: 12)
        .background(AnalysisPalette.black)
        .clipShape(Capsule())
        .overlay { Capsule().strokeBorder(Theme.border, lineWidth: 1) }
        .accessibilityElement()
        .accessibilityLabel(position.map { "Evaluation \($0.evaluationLabel)" } ?? "Evaluation pending")
        .accessibilityIdentifier(AccessibilityID.Analysis.bar)
    }
}

struct EvalChart: View {
    let positions: [PositionAnalysis]
    let ply: Int
    let select: (Int) -> Void

    var body: some View {
        let last = max(1, positions.count - 1)
        GeometryReader { proxy in
            let size = proxy.size
            let point = { (index: Int, share: Double) in
                CGPoint(x: size.width * CGFloat(index) / CGFloat(last), y: size.height * (1 - share))
            }
            ZStack(alignment: .topLeading) {
                AnalysisPalette.black
                Path { path in
                    path.move(to: CGPoint(x: 0, y: size.height))
                    for (index, position) in positions.enumerated() {
                        path.addLine(to: point(index, position.whiteShare))
                    }
                    path.addLine(to: CGPoint(x: size.width * CGFloat(positions.count - 1) / CGFloat(last), y: size.height))
                    path.closeSubpath()
                }
                .fill(AnalysisPalette.white)
                Path { path in
                    path.move(to: CGPoint(x: 0, y: size.height / 2))
                    path.addLine(to: CGPoint(x: size.width, y: size.height / 2))
                }
                .stroke(Theme.muted, style: StrokeStyle(lineWidth: 1, dash: [3, 3]))
                Rectangle()
                    .fill(Theme.primary)
                    .frame(width: 2, height: size.height)
                    .offset(x: size.width * CGFloat(min(ply, last)) / CGFloat(last) - 1)
                ForEach(Array(positions.enumerated()), id: \.offset) { index, position in
                    if let mark = position.moveClass.mark {
                        let spot = point(index, position.whiteShare)
                        Circle()
                            .fill(mark == .blunder ? Theme.destructive : Theme.warning)
                            .overlay { Circle().strokeBorder(Theme.card, lineWidth: 1) }
                            .frame(width: 8, height: 8)
                            .position(x: min(max(spot.x, 4), size.width - 4), y: min(max(spot.y, 4), size.height - 4))
                    }
                }
            }
            .contentShape(Rectangle())
            .gesture(DragGesture(minimumDistance: 0).onChanged { value in
                guard size.width > 0 else { return }
                select(Int((value.location.x / size.width * CGFloat(last)).rounded()))
            })
        }
        .frame(height: 64)
        .clipShape(.rect(cornerRadius: 8, style: .continuous))
        .overlay { RoundedRectangle(cornerRadius: 8, style: .continuous).strokeBorder(Theme.border, lineWidth: 1) }
        .accessibilityElement()
        .accessibilityLabel("Evaluation by move")
        .accessibilityValue("Move \(ply) of \(last)")
        .accessibilityAdjustableAction { direction in
            switch direction {
            case .increment: select(min(ply + 1, last))
            case .decrement: select(max(ply - 1, 0))
            default: break
            }
        }
        .accessibilityIdentifier(AccessibilityID.Analysis.chart)
    }
}

struct AnalysisPanel: View {
    let model: ReplayViewModel

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Text("Analysis").font(.axHeadline)
                Spacer()
                if let ready = model.analysisReady {
                    Text("\(ready.engine) · depth \(ready.depth)")
                        .font(.axCaption)
                        .foregroundStyle(Theme.muted)
                        .lineLimit(1)
                }
            }
            switch model.analysisState {
            case .pending:
                Label {
                    Text("Analysis pending")
                } icon: {
                    ProgressView().controlSize(.small)
                }
                .font(.axFootnote)
                .foregroundStyle(Theme.muted)
            case .failed:
                Text("Analysis unavailable for this game")
                    .font(.axFootnote)
                    .foregroundStyle(Theme.muted)
            case .error:
                HStack {
                    Text("Could not load the analysis.").font(.axFootnote)
                    Spacer()
                    Button("Try again") { Task { await model.retryAnalysis() } }
                        .buttonStyle(.bordered)
                }
            case .ready:
                if let ready = model.analysisReady {
                    EvalChart(positions: ready.positions, ply: Int(model.frame?.ply ?? 0)) { model.select(ply: $0) }
                }
                if let verdict = model.verdict {
                    verdictView(verdict)
                }
            }
        }
        .card()
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier(AccessibilityID.Analysis.panel)
    }

    private func verdictView(_ verdict: AnalysisVerdict) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(verdict.evaluation)
                .font(.axMono(15, medium: true))
                .monospacedDigit()
                .accessibilityIdentifier(AccessibilityID.Analysis.evaluation)
            Text(verdict.move ?? "Start position")
                .foregroundStyle(verdict.move == nil ? Theme.muted : Theme.foreground)
                .accessibilityIdentifier(AccessibilityID.Analysis.verdict)
            if let best = verdict.best {
                Text(best).foregroundStyle(Theme.muted)
            }
        }
        .font(.axSubheadline)
    }
}
