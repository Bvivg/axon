import SwiftUI

struct AdaptiveNavigation<Sidebar: View, Detail: View>: View {
    @ViewBuilder var sidebar: () -> Sidebar
    @ViewBuilder var detail: () -> Detail

    var body: some View {
        #if os(macOS)
        NavigationSplitView {
            sidebar()
                .navigationSplitViewColumnWidth(min: 220, ideal: 260)
        } detail: {
            NavigationStack {
                detail()
            }
        }
        #else
        NavigationStack {
            sidebar()
        }
        #endif
    }
}
