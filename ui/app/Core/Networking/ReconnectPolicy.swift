import Foundation

struct ReconnectPolicy: Equatable, Sendable {
    static let standard = ReconnectPolicy()

    var base: Duration = .seconds(1)
    var cap: Duration = .seconds(15)
    var maxAttempts = 15
    var jitter = 0.1

    func delay(forAttempt attempt: Int, unit: Double = .random(in: 0..<1)) -> Duration? {
        guard attempt >= 0, attempt < maxAttempts else { return nil }
        let exponential = base * (1 << min(attempt, 30))
        let capped = min(exponential, cap)
        guard jitter > 0 else { return capped }
        let spread = jitter * (unit * 2 - 1)
        return capped * (1 + spread)
    }
}

protocol ChannelSleeper: Sendable {
    func sleep(for duration: Duration) async throws
}

struct TaskSleeper: ChannelSleeper {
    func sleep(for duration: Duration) async throws {
        try await Task.sleep(for: duration)
    }
}
