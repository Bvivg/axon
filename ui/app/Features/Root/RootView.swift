import SwiftUI

struct RootView: View {
    var body: some View {
        AdaptiveNavigation {
            List {
                Label("Lobby", systemImage: "square.grid.2x2")
            }
            .navigationTitle("Axon")
        } detail: {
            ContentUnavailableView("Axon", systemImage: "gamecontroller")
        }
    }
}
