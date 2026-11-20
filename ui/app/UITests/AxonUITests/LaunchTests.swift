import XCTest

final class LaunchTests: XCTestCase {
    @MainActor
    func testLaunches() {
        let app = XCUIApplication()
        app.launch()
        XCTAssertTrue(app.wait(for: .runningForeground, timeout: 30))
    }
}
