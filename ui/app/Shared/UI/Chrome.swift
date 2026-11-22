import SwiftUI

extension View {
    func card(padding: CGFloat = 16, cornerRadius: CGFloat = Theme.radius) -> some View {
        surface(padding: padding, cornerRadius: cornerRadius)
    }

    func cardBackground(cornerRadius: CGFloat = Theme.radius) -> some View {
        self
            .background(Theme.card, in: .rect(cornerRadius: cornerRadius, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                    .strokeBorder(Theme.border, lineWidth: 1)
            }
    }

    func bottomActions<Actions: View>(@ViewBuilder _ actions: @escaping () -> Actions) -> some View {
        modifier(BottomActions(actions: actions))
    }
}

private struct BottomActions<Actions: View>: ViewModifier {
    let actions: () -> Actions

    func body(content: Content) -> some View {
        #if os(iOS)
        content
            .toolbar {
                ToolbarItemGroup(placement: .bottomBar) { actions() }
            }
            .toolbarVisibility(.hidden, for: .tabBar)
        #else
        content
            .safeAreaInset(edge: .bottom) {
                GlassEffectContainer {
                    HStack(spacing: 12) { actions() }
                        .buttonStyle(.glass)
                        .padding(12)
                }
            }
        #endif
    }
}
