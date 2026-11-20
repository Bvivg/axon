import Foundation
import Observation

@MainActor
@Observable
final class SettingsViewModel {
    var address: String
    private(set) var error: String?
    private(set) var isSaving = false
    @ObservationIgnored private let current: GatewayAddress
    @ObservationIgnored private let apply: @MainActor (GatewayAddress) async -> Void

    init(current: GatewayAddress, apply: @escaping @MainActor (GatewayAddress) async -> Void) {
        self.current = current
        self.apply = apply
        address = current.host
    }

    var changesAccount: Bool {
        (try? GatewayAddress(parsing: address)).map { $0 != current } ?? false
    }

    func save() async -> Bool {
        do {
            let parsed = try GatewayAddress(parsing: address)
            error = nil
            isSaving = true
            defer { isSaving = false }
            await apply(parsed)
            return true
        } catch {
            self.error = switch error {
            case .empty: "Enter the gateway address."
            case .unsupportedScheme: "Use an http:// or https:// address."
            case .missingHost: "The address has no host."
            case .hasPath: "Use only scheme, host and port, without a path."
            }
            return false
        }
    }
}
