import SwiftUI

@main
struct AxonApp: App {
    @State private var app: AppModel

    init() {
        let arguments = ProcessInfo.processInfo.arguments
        let storage = UserDefaultsStorage()
        let tokens = KeychainTokenStore(account: "default")
        if let index = arguments.firstIndex(of: AccessibilityID.Launch.resetArgument),
           arguments.indices.contains(index + 1),
           arguments[index + 1] == "YES" {
            try? tokens.clear()
        }
        Theme.configureAppearance()
        let settings = GatewaySettings(storage: storage)
        let vault = TokenVault(store: tokens)
        _app = State(initialValue: AppModel(settings: settings, vault: vault, build: LiveServices.make))
    }

    var body: some Scene {
        WindowGroup {
            RootView(app: app)
        }
        #if os(macOS)
        .defaultSize(width: 1100, height: 760)
        #endif
    }
}
