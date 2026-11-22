import SwiftUI

struct RootView: View {
    @Bindable var app: AppModel
    @State private var showsSettings = false
    @AppStorage(Preferences.appearance) private var appearance = AppearanceChoice.system

    var body: some View {
        Group {
            switch app.phase {
            case .launching:
                ZStack {
                    Theme.background.ignoresSafeArea()
                    AxonAppIcon(size: 72)
                }
            case .signedOut:
                AuthView(services: app.services, onSignedIn: app.signedIn, onSettings: { showsSettings = true })
            case .signedIn(let user):
                HomeView(app: app, user: user, onSettings: { showsSettings = true })
                    .id(user.id)
            }
        }
        .sheet(isPresented: $showsSettings) {
            SettingsView(current: app.settings.address) { address in
                await app.updateGateway(address)
            }
        }
        .font(.axBody)
        .tint(Theme.primary)
        .preferredColorScheme(appearance.scheme)
        .task { await app.start() }
    }
}
