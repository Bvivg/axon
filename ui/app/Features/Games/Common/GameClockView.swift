import SwiftUI

enum ClockReadout {
    static let lowThreshold: Duration = .seconds(10)

    static func remaining(_ remaining: Duration, running: Bool, sampledAt: Date, at now: Date) -> Duration {
        guard running else { return max(remaining, .zero) }
        let elapsed = max(now.timeIntervalSince(sampledAt), 0)
        return max(remaining - .seconds(elapsed), .zero)
    }

    static func wholeSeconds(_ duration: Duration) -> Int64 {
        let components = max(duration, .zero).components
        return components.seconds + (components.attoseconds > 0 ? 1 : 0)
    }

    static func text(_ duration: Duration) -> String {
        let seconds = wholeSeconds(duration)
        let tail = seconds % 60
        return "\(seconds / 60):\(tail < 10 ? "0" : "")\(tail)"
    }

    static func isLow(_ duration: Duration, running: Bool) -> Bool {
        duration <= lowThreshold && (running || duration <= .zero)
    }

    static func firstTick(remaining: Duration, sampledAt: Date) -> Date {
        let components = max(remaining, .zero).components
        let fraction = Double(components.attoseconds) / 1e18
        return sampledAt.addingTimeInterval(fraction + 0.001 - 1)
    }
}

struct GameClockView: View {
    private let remaining: Duration
    private let running: Bool
    private let sampledAt: Date

    @Environment(\.colorScheme) private var colorScheme

    init(remaining: Duration, running: Bool, sampledAt: Date) {
        self.remaining = remaining
        self.running = running
        self.sampledAt = sampledAt
    }

    var body: some View {
        if running {
            TimelineView(.periodic(from: ClockReadout.firstTick(remaining: remaining, sampledAt: sampledAt), by: 1)) { context in
                face(ClockReadout.remaining(remaining, running: true, sampledAt: sampledAt, at: context.date))
            }
        } else {
            face(ClockReadout.remaining(remaining, running: false, sampledAt: sampledAt, at: sampledAt))
        }
    }

    private func face(_ value: Duration) -> some View {
        let palette = BoardPalette.resolve(colorScheme)
        let low = ClockReadout.isLow(value, running: running)
        let text = ClockReadout.text(value)
        return Text(text)
            .font(.axMono(20, medium: true, relativeTo: .title3))
            .monospacedDigit()
            .foregroundStyle(running || low ? AnyShapeStyle(palette.onAccent) : AnyShapeStyle(.secondary))
            .padding(.horizontal, 14)
            .frame(minWidth: 84, minHeight: 38)
            .background {
                Capsule()
                    .fill(low ? AnyShapeStyle(palette.danger) : running ? AnyShapeStyle(palette.violet) : AnyShapeStyle(.quaternary))
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(text)
            .accessibilityValue(running ? "Running" : "Stopped")
            .accessibilityAddTraits(.updatesFrequently)
    }
}
