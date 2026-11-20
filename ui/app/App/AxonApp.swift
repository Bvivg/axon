import SwiftUI

@main
struct AxonApp: App {
    var body: some Scene {
        WindowGroup {
            RootView()
        }
        #if os(macOS)
        .defaultSize(width: 1100, height: 760)
        #endif
    }
}
