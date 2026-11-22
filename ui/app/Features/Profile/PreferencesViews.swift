import SwiftUI
import UserNotifications

enum NotificationAccess: Equatable {
    case checking
    case off
    case on
    case blocked
}

struct NotificationsView: View {
    @AppStorage(Preferences.callSounds) private var callSounds = true
    @AppStorage(Preferences.messageSounds) private var messageSounds = true
    @AppStorage(Preferences.messageAlerts) private var messageAlerts = true
    @State private var access = NotificationAccess.checking
    @Environment(\.openURL) private var openURL

    var body: some View {
        ProfileScroll(title: ProfilePage.notifications.title) {
            ProfileSection("Sounds in Axon") {
                Toggle("Call sounds", isOn: $callSounds)
                    .padding(.vertical, 4)
                Divider().overlay(Theme.line)
                Toggle("Message sounds", isOn: $messageSounds)
                    .padding(.vertical, 4)
            }
            ProfileSection("This device") {
                HStack(spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("New messages and moves")
                            .font(.geist(15, .medium, relativeTo: .body))
                        Text(caption)
                            .font(.axMono(11, relativeTo: .caption2))
                            .foregroundStyle(Theme.muted)
                    }
                    Spacer(minLength: 8)
                    control
                }
            }
        }
        .font(.axBody)
        .tint(Theme.primary)
        .task { await refresh() }
    }

    private var caption: String {
        switch access {
        case .checking: "Checking…"
        case .blocked: "Blocked in Settings"
        case .off, .on: "While Axon is running"
        }
    }

    @ViewBuilder
    private var control: some View {
        switch access {
        case .checking:
            ProgressView().controlSize(.small)
        case .off:
            Button("Turn on") { Task { await request() } }
                .buttonStyle(.borderedProminent)
                .buttonBorderShape(.roundedRectangle(radius: 8))
        case .on:
            Toggle("Alerts", isOn: $messageAlerts).labelsHidden()
        case .blocked:
            Button("Settings") {
                if let url = Preferences.notificationSettingsURL { openURL(url) }
            }
            .buttonStyle(.bordered)
        }
    }

    private func refresh() async {
        let status = await UNUserNotificationCenter.current().notificationSettings().authorizationStatus
        access = switch status {
        case .authorized, .provisional, .ephemeral: .on
        case .denied: .blocked
        default: .off
        }
    }

    private func request() async {
        _ = try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
        messageAlerts = true
        await refresh()
    }
}

enum AppearanceChoice: String, CaseIterable, Identifiable {
    case light
    case dark
    case system

    var id: String { rawValue }

    var title: String { rawValue.capitalized }

    var scheme: ColorScheme? {
        switch self {
        case .light: .light
        case .dark: .dark
        case .system: nil
        }
    }
}

enum Preferences {
    static let appearance = "appearance"
    static let callSounds = "sounds.calls"
    static let messageSounds = "sounds.messages"
    static let messageAlerts = "alerts.messages"

    static var notificationSettingsURL: URL? {
        #if os(iOS)
        URL(string: UIApplication.openNotificationSettingsURLString)
        #else
        URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension")
        #endif
    }
}

struct AppearanceView: View {
    @AppStorage(Preferences.appearance) private var appearance = AppearanceChoice.system

    var body: some View {
        ProfileScroll(title: ProfilePage.appearance.title) {
            Eyebrow("Theme").padding(.horizontal, 4)
            HStack(spacing: 12) {
                ForEach(AppearanceChoice.allCases) { choice in
                    Button {
                        withAnimation(.snappy) { appearance = choice }
                    } label: {
                        VStack(alignment: .leading, spacing: 8) {
                            HStack(spacing: 0) {
                                if choice != .dark { ThemePreview(scheme: .light) }
                                if choice != .light { ThemePreview(scheme: .dark) }
                            }
                            .frame(height: 96)
                            .clipShape(.rect(cornerRadius: 12, style: .continuous))
                            .overlay {
                                RoundedRectangle(cornerRadius: 12, style: .continuous)
                                    .strokeBorder(selected(choice) ? Theme.primary : Theme.border, lineWidth: selected(choice) ? 2 : 1)
                            }
                            HStack(spacing: 8) {
                                Circle()
                                    .strokeBorder(selected(choice) ? Theme.primary : Theme.muted, lineWidth: selected(choice) ? 5 : 1.5)
                                    .frame(width: 16, height: 16)
                                Text(choice.title)
                                    .font(.geist(14, .medium, relativeTo: .subheadline))
                                    .foregroundStyle(Theme.foreground)
                            }
                        }
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(selected(choice) ? .isSelected : [])
                    .accessibilityIdentifier(AccessibilityID.Profile.theme(choice.rawValue))
                }
            }
        }
    }

    private func selected(_ choice: AppearanceChoice) -> Bool {
        appearance == choice
    }
}

private struct ThemePreview: View {
    let scheme: ColorScheme

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Capsule().fill(Theme.foreground).frame(height: 8).scaleEffect(x: 0.6, anchor: .leading)
            Capsule().fill(Theme.border).frame(height: 6).scaleEffect(x: 0.85, anchor: .leading)
            Capsule().fill(Theme.border).frame(height: 6).scaleEffect(x: 0.7, anchor: .leading)
            Spacer(minLength: 0)
            RoundedRectangle(cornerRadius: 3).fill(Theme.primary).frame(height: 14).scaleEffect(x: 0.5, anchor: .leading)
        }
        .padding(8)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.background)
        .environment(\.colorScheme, scheme)
    }
}
