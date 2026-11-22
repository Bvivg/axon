import SwiftUI

struct ProfileView: View {
    let model: ProfileViewModel
    let gateway: GatewayAddress
    let onSettings: () -> Void

    var body: some View {
        List {
            Section {
                NavigationLink(value: Route.profile(.account)) {
                    HStack(spacing: 14) {
                        Avatar(name: model.user.personName, url: model.user.avatarLink, size: 52, cornerRadius: 16)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(model.user.personName)
                                .font(.geist(17, .semibold, relativeTo: .headline))
                                .foregroundStyle(Theme.foreground)
                                .lineLimit(1)
                            Text(model.user.handle)
                                .font(.axFootnote)
                                .foregroundStyle(Theme.muted)
                                .lineLimit(1)
                        }
                    }
                    .padding(.vertical, 6)
                }
                .accessibilityIdentifier(AccessibilityID.Profile.page(ProfilePage.account.rawValue))
                .plainRow()
            }
            Section {
                ForEach(ProfilePage.allCases.filter { $0 != .account }, id: \.self) { page in
                    NavigationLink(value: Route.profile(page)) {
                        CatalogRow(title: page.title) {
                            IconTile(size: 36) {
                                Image(systemName: page.symbol).font(.system(size: 16))
                            }
                        }
                    }
                    .accessibilityIdentifier(AccessibilityID.Profile.page(page.rawValue))
                    .plainRow()
                }
            }
            Section {
                Button(action: onSettings) {
                    CatalogRow(title: "Gateway", subtitle: gateway.host) {
                        IconTile(size: 36) {
                            Image(systemName: "server.rack").font(.system(size: 15))
                        }
                    }
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier(AccessibilityID.Settings.open)
                .plainRow()
            } header: {
                Eyebrow("Server")
            }
        }
        .listStyle(.plain)
        .themedList(Theme.sidebar)
        .rootTitle("Profile")
        .accessibilityIdentifier(AccessibilityID.Profile.root)
    }
}

extension ProfilePage {
    var title: String {
        switch self {
        case .account: "My profile"
        case .personal: "Personal info"
        case .sessions: "Sessions"
        case .notifications: "Notifications"
        case .appearance: "Appearance"
        }
    }

    var symbol: String {
        switch self {
        case .account: "person"
        case .personal: "person.text.rectangle"
        case .sessions: "laptopcomputer.and.iphone"
        case .notifications: "bell"
        case .appearance: "paintpalette"
        }
    }
}

struct ProfilePageView: View {
    let page: ProfilePage
    let model: ProfileViewModel
    let onSignOut: () -> Void

    var body: some View {
        switch page {
        case .account: AccountView(model: model, onSignOut: onSignOut)
        case .personal: PersonalInfoView(model: model)
        case .sessions: SessionsView(model: model)
        case .notifications: NotificationsView()
        case .appearance: AppearanceView()
        }
    }
}

struct ProfileScroll<Content: View>: View {
    let title: String
    @ViewBuilder let content: () -> Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                content()
            }
            .padding(16)
            .frame(maxWidth: 640)
            .frame(maxWidth: .infinity)
        }
        .background(Theme.background)
        .navigationTitle(title)
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
    }
}

struct ProfileSection<Content: View, Aside: View>: View {
    let title: String
    @ViewBuilder let content: () -> Content
    @ViewBuilder var aside: () -> Aside

    init(_ title: String, @ViewBuilder content: @escaping () -> Content, @ViewBuilder aside: @escaping () -> Aside = { EmptyView() }) {
        self.title = title
        self.content = content
        self.aside = aside
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Eyebrow(title)
                Spacer()
                aside()
            }
            .padding(.horizontal, 4)
            VStack(alignment: .leading, spacing: 0) {
                content()
            }
            .surface(padding: 16)
        }
    }
}

struct StatusBadge: View {
    enum Tone {
        case success
        case warning
        case signal
        case neutral
    }

    let text: String
    var tone: Tone = .neutral
    var dot = false

    private var color: Color {
        switch tone {
        case .success: Theme.success
        case .warning: Theme.warning
        case .signal: Theme.primary
        case .neutral: Theme.muted
        }
    }

    var body: some View {
        HStack(spacing: 5) {
            if dot {
                Circle().fill(color).frame(width: 6, height: 6)
            }
            Text(text)
                .font(.geist(11, .medium, relativeTo: .caption2))
        }
        .foregroundStyle(tone == .neutral ? Theme.muted : color)
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(tone == .neutral ? Color.clear : color.opacity(0.12), in: .capsule)
        .overlay {
            Capsule().strokeBorder(tone == .neutral ? Theme.border : color.opacity(0.3), lineWidth: 1)
        }
    }
}
