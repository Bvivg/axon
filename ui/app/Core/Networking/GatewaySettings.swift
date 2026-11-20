import Foundation
import Observation

enum GatewayAddressError: Error, Equatable {
    case empty
    case unsupportedScheme
    case missingHost
    case hasPath
}

struct GatewayAddress: Hashable, Sendable {
    static let fallback = GatewayAddress(validated: URL(string: "http://localhost:18080")!)

    let url: URL

    private init(validated url: URL) {
        self.url = url
    }

    init(parsing text: String) throws(GatewayAddressError) {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { throw .empty }
        guard var components = URLComponents(string: trimmed),
              let scheme = components.scheme?.lowercased(), ["http", "https"].contains(scheme) else {
            throw .unsupportedScheme
        }
        guard let host = components.host, !host.isEmpty else { throw .missingHost }
        let path = components.path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        guard path.isEmpty, components.query == nil, components.fragment == nil else { throw .hasPath }
        components.scheme = scheme
        components.path = ""
        guard let url = components.url else { throw .missingHost }
        self.url = url
    }

    var host: String { url.absoluteString }

    var gameSocketURL: URL {
        var components = URLComponents(url: url, resolvingAgainstBaseURL: false)!
        components.scheme = components.scheme == "https" ? "wss" : "ws"
        components.path = "/ws/game"
        return components.url!
    }
}

protocol SettingsStorage: AnyObject, Sendable {
    func string(forKey key: String) -> String?
    func set(_ value: String?, forKey key: String)
}

final class UserDefaultsStorage: SettingsStorage, @unchecked Sendable {
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func string(forKey key: String) -> String? {
        defaults.string(forKey: key)
    }

    func set(_ value: String?, forKey key: String) {
        defaults.set(value, forKey: key)
    }
}

@MainActor
@Observable
final class GatewaySettings {
    static let addressKey = "gatewayBaseURL"
    static let deviceKey = "gatewayDeviceID"

    private(set) var address: GatewayAddress
    let deviceID: String
    @ObservationIgnored private let storage: any SettingsStorage

    init(storage: any SettingsStorage = UserDefaultsStorage()) {
        self.storage = storage
        address = (try? GatewayAddress(parsing: storage.string(forKey: Self.addressKey) ?? "")) ?? .fallback
        if let device = storage.string(forKey: Self.deviceKey), UUID(uuidString: device) != nil {
            deviceID = device
        } else {
            let device = UUID().uuidString.lowercased()
            storage.set(device, forKey: Self.deviceKey)
            deviceID = device
        }
    }

    func update(_ address: GatewayAddress) {
        guard address != self.address else { return }
        storage.set(address.host, forKey: Self.addressKey)
        self.address = address
    }
}
