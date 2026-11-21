import SwiftUI

struct AdaptiveNavigation<Route: Hashable, Sidebar: View, Placeholder: View, Destination: View>: View {
    @Binding var path: [Route]
    @ViewBuilder var sidebar: () -> Sidebar
    @ViewBuilder var placeholder: () -> Placeholder
    @ViewBuilder var destination: (Route) -> Destination

    var body: some View {
        #if os(macOS)
        NavigationSplitView {
            sidebar()
                .navigationSplitViewColumnWidth(min: 300, ideal: 340, max: 420)
        } detail: {
            NavigationStack(path: $path) {
                placeholder()
                    .navigationDestination(for: Route.self, destination: destination)
            }
        }
        #else
        NavigationStack(path: $path) {
            sidebar()
                .navigationDestination(for: Route.self, destination: destination)
        }
        #endif
    }
}

extension Array {
    mutating func open(_ route: Element) {
        #if os(macOS)
        self = [route]
        #else
        append(route)
        #endif
    }
}
