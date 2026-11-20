import Foundation
import Security
import Synchronization

struct AuthTokens: Codable, Equatable, Sendable {
    let accessToken: String
    let refreshToken: String
    let expiresAt: Date

    init(accessToken: String, refreshToken: String, expiresAt: Date) {
        self.accessToken = accessToken
        self.refreshToken = refreshToken
        self.expiresAt = expiresAt
    }

    init?(_ pair: TokenPair, previousRefreshToken: String? = nil, now: Date = .now) {
        let refresh = pair.refreshToken.isEmpty ? previousRefreshToken ?? "" : pair.refreshToken
        guard !pair.accessToken.isEmpty, !refresh.isEmpty else { return nil }
        self.init(accessToken: pair.accessToken, refreshToken: refresh, expiresAt: now.addingTimeInterval(TimeInterval(pair.expiresIn)))
    }

    func expires(within margin: TimeInterval, now: Date = .now) -> Bool {
        expiresAt.timeIntervalSince(now) <= margin
    }
}

enum TokenStoreError: Error, Equatable {
    case keychain(OSStatus)
    case corrupted
}

protocol AuthTokenStore: Sendable {
    func load() throws(TokenStoreError) -> AuthTokens?
    func save(_ tokens: AuthTokens) throws(TokenStoreError)
    func clear() throws(TokenStoreError)
}

final class InMemoryTokenStore: AuthTokenStore {
    private let tokens: Mutex<AuthTokens?>

    init(_ tokens: AuthTokens? = nil) {
        self.tokens = Mutex(tokens)
    }

    func load() -> AuthTokens? {
        tokens.withLock { $0 }
    }

    func save(_ tokens: AuthTokens) {
        self.tokens.withLock { $0 = tokens }
    }

    func clear() {
        tokens.withLock { $0 = nil }
    }
}

struct KeychainTokenStore: AuthTokenStore {
    let service: String
    let account: String

    init(service: String = "dev.axon.Axon.tokens", account: String) {
        self.service = service
        self.account = account
    }

    private var query: [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }

    func load() throws(TokenStoreError) -> AuthTokens? {
        var request = query
        request[kSecReturnData as String] = true
        request[kSecMatchLimit as String] = kSecMatchLimitOne
        var item: CFTypeRef?
        let status = SecItemCopyMatching(request as CFDictionary, &item)
        switch status {
        case errSecSuccess:
            guard let data = item as? Data else { throw .corrupted }
            do {
                return try JSONDecoder().decode(AuthTokens.self, from: data)
            } catch {
                throw .corrupted
            }
        case errSecItemNotFound:
            return nil
        default:
            throw .keychain(status)
        }
    }

    func save(_ tokens: AuthTokens) throws(TokenStoreError) {
        let data: Data
        do {
            data = try JSONEncoder().encode(tokens)
        } catch {
            throw .corrupted
        }
        let attributes: [String: Any] = [
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        let status = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            let added = SecItemAdd(query.merging(attributes) { $1 } as CFDictionary, nil)
            guard added == errSecSuccess else { throw .keychain(added) }
            return
        }
        guard status == errSecSuccess else { throw .keychain(status) }
    }

    func clear() throws(TokenStoreError) {
        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw .keychain(status) }
    }
}
