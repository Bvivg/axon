import Foundation
import Testing
@testable import Axon

struct ReconnectPolicyTests {
    @Test
    func standardPolicyMatchesTheWebClient() {
        let policy = ReconnectPolicy.standard
        #expect(policy.base == .seconds(1))
        #expect(policy.cap == .seconds(15))
        #expect(policy.maxAttempts == 15)
        #expect(policy.jitter > 0 && policy.jitter <= 0.2)
    }

    @Test(arguments: [(0, 1), (1, 2), (2, 4), (3, 8), (4, 15), (5, 15), (14, 15)])
    func doublesUpToTheCap(attempt: Int, seconds: Int) {
        let policy = ReconnectPolicy(jitter: 0)
        #expect(policy.delay(forAttempt: attempt) == .seconds(seconds))
    }

    @Test(arguments: [-1, 15, 16, 1000])
    func stopsAfterTheLastAttempt(attempt: Int) {
        #expect(ReconnectPolicy.standard.delay(forAttempt: attempt) == nil)
    }

    @Test
    func jitterStaysSmallAndCentred() {
        let policy = ReconnectPolicy.standard
        #expect(Self.near(policy.delay(forAttempt: 2, unit: 0.5), .seconds(4)))
        #expect(Self.near(policy.delay(forAttempt: 2, unit: 0), .seconds(3.6)))
        #expect(Self.near(policy.delay(forAttempt: 4, unit: 1), .seconds(16.5)))
        for _ in 0..<200 {
            let delay = policy.delay(forAttempt: 3) ?? .zero
            #expect(delay >= .seconds(7.2) && delay <= .seconds(8.8))
        }
    }

    static func near(_ actual: Duration?, _ expected: Duration) -> Bool {
        guard let actual else { return false }
        return abs((actual - expected) / .milliseconds(1)) < 0.001
    }
}
