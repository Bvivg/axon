import CryptoKit
import Foundation

enum UploadPurpose: String, Sendable {
    case media
    case file
    case voice
}

enum UploadStage: Equatable, Sendable {
    case hashing
    case uploading(Double)
    case processing
}

struct UploadedFile: Equatable, Sendable {
    let id: String
    let kind: String
}

protocol MediaAPI: Sendable {
    func link(_ raw: String) -> URL?
    func upload(
        _ file: URL,
        name: String,
        as purpose: UploadPurpose,
        progress: @escaping @Sendable (UploadStage) -> Void
    ) async throws(APIError) -> UploadedFile
    func data(_ link: URL) async throws(APIError) -> Data
    func download(_ link: URL, named name: String) async throws(APIError) -> URL
    func playbackHeaders(for link: URL) async -> [String: String]
}

struct UploadReady: Decodable, Equatable, Sendable {
    let uploadID: String
    let kind: String

    enum CodingKeys: String, CodingKey {
        case uploadID = "upload_id"
        case kind
    }
}

struct UploadSession: Decodable, Equatable, Sendable {
    let id: String
    let status: String
    let partSize: Int64
    let partCount: Int
    let parts: [Int]?
    let failure: String?
    let upload: UploadReady?

    enum CodingKeys: String, CodingKey {
        case id, status, parts, failure, upload
        case partSize = "part_size"
        case partCount = "part_count"
    }

    func missing() -> [Int] {
        let done = Set(parts ?? [])
        return partCount > 0 ? (1...partCount).filter { !done.contains($0) } : []
    }
}

enum UploadMessages {
    static func limit(_ purpose: UploadPurpose) -> String {
        switch purpose {
        case .media: "Photos can be up to 25 MB and videos up to 200 MB."
        case .file: "Files can be up to 200 MB."
        case .voice: "Voice messages can be up to 10 MB."
        }
    }

    static func failure(_ reason: String?, _ purpose: UploadPurpose) -> String {
        switch reason {
        case "hash_mismatch": "The file changed while it was being uploaded. Attach it again."
        case "too_large": "That file is too large. \(limit(purpose))"
        case "animation_too_large": "This animation is too long or too large to send as a photo. Send it as a file instead."
        case "unsupported": purpose == .media
            ? "This file is not a photo or video we can open. Send it as a file instead."
            : "This recording could not be read."
        case "timeout": "Processing took too long. Try a shorter video."
        default: "The upload failed. Try again."
        }
    }

    static func status(_ code: Int, body: Data, purpose: UploadPurpose, retryAfter: String?) -> String {
        switch code {
        case 413:
            let daily = (try? JSONSerialization.jsonObject(with: body) as? [String: Any])?["code"] as? String == "resource_exhausted"
            return daily ? "You have reached today's upload limit. Try again tomorrow." : "That file is too large. \(limit(purpose))"
        case 429:
            return "Too many uploads at once. Try again in \(Int(retryAfter ?? "") ?? 5) seconds."
        default:
            return "The upload failed. Try again."
        }
    }
}

final class LiveMediaAPI: MediaAPI, @unchecked Sendable {
    private struct Reply {
        let data: Data
        let status: Int
        let retryAfter: String?
    }

    private struct Failure: Error {
        let status: Int?
        let body: Data
        let retryAfter: String?

        var retriable: Bool { status == nil || (status ?? 0) >= 500 }
    }

    static let partAttempts = 4

    private let address: GatewayAddress
    private let vault: TokenVault
    private let session: URLSession
    private let cache = NSCache<NSURL, NSData>()

    init(address: GatewayAddress, vault: TokenVault) {
        self.address = address
        self.vault = vault
        let configuration = URLSessionConfiguration.ephemeral
        configuration.httpShouldSetCookies = false
        configuration.httpCookieAcceptPolicy = .never
        configuration.httpCookieStorage = nil
        configuration.httpAdditionalHeaders = ["User-Agent": ConnectClient.userAgent]
        session = URLSession(configuration: configuration)
        cache.totalCostLimit = 96 * 1024 * 1024
    }

    func link(_ raw: String) -> URL? {
        address.media(raw)
    }

    func data(_ link: URL) async throws(APIError) -> Data {
        if let cached = cache.object(forKey: link as NSURL) { return cached as Data }
        do {
            let data = address.sameOrigin(link) ? try await send(URLRequest(url: link)) : try await fetch(link)
            cache.setObject(data as NSData, forKey: link as NSURL, cost: data.count)
            return data
        } catch let failure as Failure {
            throw APIError(failure.status == 404 ? .notFound : .unavailable, "Could not load the file.")
        } catch {
            throw APIError(.cancelled)
        }
    }

    func download(_ link: URL, named name: String) async throws(APIError) -> URL {
        let data = try await data(link)
        let folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString, directoryHint: .isDirectory)
        let safe = name.replacingOccurrences(of: "/", with: "-")
        let target = folder.appending(path: safe.isEmpty ? "file" : safe)
        do {
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            try data.write(to: target)
        } catch {
            throw APIError(.unknown, "Could not save the file.")
        }
        return target
    }

    func playbackHeaders(for link: URL) async -> [String: String] {
        guard address.sameOrigin(link) else { return [:] }
        var token = await vault.validAccessToken()
        if token == nil { token = await vault.accessToken }
        return token.map { ["Authorization": "Bearer \($0)"] } ?? [:]
    }

    func upload(
        _ file: URL,
        name: String,
        as purpose: UploadPurpose,
        progress: @escaping @Sendable (UploadStage) -> Void
    ) async throws(APIError) -> UploadedFile {
        var created: String?
        do {
            progress(.hashing)
            let (digest, size) = try await Task.detached { try Self.digest(of: file) }.value
            try Task.checkCancellation()
            var session = try await start(name: name, size: size, digest: digest, purpose: purpose)
            created = session.id
            if session.status == "ready", let upload = session.upload {
                progress(.uploading(1))
                return UploadedFile(id: upload.uploadID, kind: upload.kind)
            }
            session = try await sendParts(of: file, size: size, session: session, progress: progress)
            progress(.processing)
            session = try await decode(send(request("\(session.id)/complete", method: "POST")))
            while session.status == "processing" {
                try Task.checkCancellation()
                var wait = request(session.id, method: "GET")
                wait.url = wait.url?.appending(queryItems: [URLQueryItem(name: "wait", value: "1")])
                session = try await decode(send(wait))
            }
            guard session.status == "ready", let upload = session.upload else {
                throw APIError(.failedPrecondition, UploadMessages.failure(session.failure, purpose))
            }
            return UploadedFile(id: upload.uploadID, kind: upload.kind)
        } catch let error as APIError {
            throw error
        } catch let failure as Failure {
            let message = failure.status.map {
                UploadMessages.status($0, body: failure.body, purpose: purpose, retryAfter: failure.retryAfter)
            } ?? "Could not reach the server. Check the connection and try again."
            throw APIError(failure.status == nil ? .unavailable : .failedPrecondition, message)
        } catch is CancellationError {
            if let created { withdraw(created) }
            throw APIError(.cancelled, "The upload was cancelled.")
        } catch {
            throw APIError(.unknown, "The upload failed. Try again.")
        }
    }

    private func start(name: String, size: Int64, digest: String, purpose: UploadPurpose) async throws -> UploadSession {
        var start = request("", method: "POST")
        start.setValue("application/json", forHTTPHeaderField: "Content-Type")
        start.httpBody = try JSONSerialization.data(withJSONObject: [
            "as": purpose.rawValue, "name": name, "size": size, "source_sha256": digest,
        ])
        return try decode(await send(start))
    }

    private func sendParts(
        of file: URL,
        size: Int64,
        session: UploadSession,
        progress: @escaping @Sendable (UploadStage) -> Void
    ) async throws -> UploadSession {
        var current = session
        var round = 0
        while true {
            let missing = current.missing()
            let pending = missing.reduce(Int64(0)) { $0 + min(current.partSize, max(0, size - Int64($1 - 1) * current.partSize)) }
            var sent = size - pending
            progress(.uploading(Self.fraction(sent, of: size)))
            do {
                for number in missing {
                    try Task.checkCancellation()
                    let part = try Self.read(file, offset: Int64(number - 1) * current.partSize, length: current.partSize)
                    var put = request("\(current.id)/parts/\(number)", method: "PUT")
                    put.setValue("application/octet-stream", forHTTPHeaderField: "Content-Type")
                    put.httpBody = part
                    _ = try await send(put)
                    sent += Int64(part.count)
                    progress(.uploading(Self.fraction(sent, of: size)))
                }
                return current
            } catch let failure as Failure where failure.retriable && round < Self.partAttempts {
                round += 1
                try await Task.sleep(for: .seconds(1 << (round - 1)))
                current = try await decode(send(request(current.id, method: "GET")))
            }
        }
    }

    private func fetch(_ link: URL) async throws -> Data {
        do {
            let (data, response) = try await session.data(from: link)
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            return try check(Reply(data: data, status: status, retryAfter: nil))
        } catch let failure as Failure {
            throw failure
        } catch {
            throw Failure(status: nil, body: Data(), retryAfter: nil)
        }
    }

    private func withdraw(_ id: String) {
        let request = request(id, method: "DELETE")
        Task.detached { [self] in _ = try? await send(request) }
    }

    private func request(_ path: String, method: String) -> URLRequest {
        var request = URLRequest(url: path.isEmpty ? address.uploadsURL : address.uploadsURL.appending(path: path))
        request.httpMethod = method
        return request
    }

    private func send(_ request: URLRequest) async throws -> Data {
        var token = await vault.validAccessToken()
        if token == nil { token = await vault.accessToken }
        guard let token else { throw Failure(status: 401, body: Data(), retryAfter: nil) }
        let reply = try await perform(request, token: token)
        if reply.status == 401, let renewed = await vault.refresh(rejected: token) {
            return try check(await perform(request, token: renewed))
        }
        return try check(reply)
    }

    private func perform(_ request: URLRequest, token: String) async throws -> Reply {
        var request = request
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        do {
            let (data, response) = try await session.data(for: request)
            let http = response as? HTTPURLResponse
            return Reply(data: data, status: http?.statusCode ?? 0, retryAfter: http?.value(forHTTPHeaderField: "Retry-After"))
        } catch let error as URLError where error.code == .cancelled {
            throw CancellationError()
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            throw Failure(status: nil, body: Data(), retryAfter: nil)
        }
    }

    private func check(_ reply: Reply) throws -> Data {
        guard (200..<300).contains(reply.status) else {
            throw Failure(status: reply.status, body: reply.data, retryAfter: reply.retryAfter)
        }
        return reply.data
    }

    private func decode(_ data: Data) throws -> UploadSession {
        try JSONDecoder().decode(UploadSession.self, from: data)
    }

    static func fraction(_ sent: Int64, of size: Int64) -> Double {
        size > 0 ? min(1, max(0, Double(sent) / Double(size))) : 1
    }

    static func digest(of file: URL) throws -> (String, Int64) {
        let handle = try FileHandle(forReadingFrom: file)
        defer { try? handle.close() }
        var hasher = SHA256()
        var size: Int64 = 0
        while let chunk = try handle.read(upToCount: 2 * 1024 * 1024), !chunk.isEmpty {
            hasher.update(data: chunk)
            size += Int64(chunk.count)
        }
        let hex = hasher.finalize().map { String(format: "%02x", $0) }.joined()
        return (hex, size)
    }

    static func read(_ file: URL, offset: Int64, length: Int64) throws -> Data {
        let handle = try FileHandle(forReadingFrom: file)
        defer { try? handle.close() }
        try handle.seek(toOffset: UInt64(offset))
        return try handle.read(upToCount: Int(length)) ?? Data()
    }
}
