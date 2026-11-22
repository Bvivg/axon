import Foundation
import UserNotifications

@MainActor
protocol ChatAlerting: AnyObject {
    func activate()
    func post(title: String, body: String, room: String)
}

@MainActor
final class LocalChatAlerts: NSObject, ChatAlerting, UNUserNotificationCenterDelegate {
    nonisolated static let roomKey = "room"

    var onOpen: ((String) -> Void)?
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        super.init()
    }

    func activate() {
        UNUserNotificationCenter.current().delegate = self
    }

    private var enabled: Bool { defaults.object(forKey: Preferences.messageAlerts) as? Bool ?? true }

    private var sounds: Bool { defaults.object(forKey: Preferences.messageSounds) as? Bool ?? true }

    func post(title: String, body: String, room: String) {
        guard enabled else { return }
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = body
        content.threadIdentifier = room
        content.userInfo = [Self.roomKey: room]
        if sounds { content.sound = .default }
        let request = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        Task {
            let center = UNUserNotificationCenter.current()
            if await center.notificationSettings().authorizationStatus == .notDetermined {
                _ = try? await center.requestAuthorization(options: [.alert, .sound, .badge])
            }
            try? await center.add(request)
        }
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        [.banner, .list, .sound]
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse
    ) async {
        guard let room = response.notification.request.content.userInfo[Self.roomKey] as? String else { return }
        await MainActor.run { onOpen?(room) }
    }
}
