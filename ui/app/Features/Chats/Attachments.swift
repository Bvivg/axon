import AVFoundation
import CoreTransferable
import Foundation
import ImageIO
import UniformTypeIdentifiers

struct PendingAttachment: Identifiable, Equatable {
    enum Phase: Equatable {
        case uploading(UploadStage)
        case ready(String)
        case failed(String)
    }

    let id: String
    let file: URL
    let name: String
    let purpose: UploadPurpose
    var phase: Phase

    var uploadedID: String? {
        if case .ready(let id) = phase { return id }
        return nil
    }

    var isImage: Bool { UTType(filenameExtension: file.pathExtension)?.conforms(to: .image) == true }

    var isVideo: Bool { UTType(filenameExtension: file.pathExtension)?.conforms(to: .movie) == true }

    var progress: Double? {
        switch phase {
        case .uploading(.uploading(let fraction)): fraction
        case .uploading: nil
        case .ready: 1
        case .failed: nil
        }
    }
}

enum Staging {
    static func folder() -> URL {
        let folder = FileManager.default.temporaryDirectory.appending(path: "outbox/\(UUID().uuidString)", directoryHint: .isDirectory)
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        return folder
    }

    static func copy(_ source: URL, name: String? = nil) -> URL? {
        let scoped = source.startAccessingSecurityScopedResource()
        defer { if scoped { source.stopAccessingSecurityScopedResource() } }
        let target = folder().appending(path: name ?? source.lastPathComponent)
        do {
            try FileManager.default.copyItem(at: source, to: target)
            return target
        } catch {
            return nil
        }
    }

    static func prepareImage(_ file: URL) -> URL {
        let type = UTType(filenameExtension: file.pathExtension)
        let heic = type?.conforms(to: .heic) == true || type?.conforms(to: .heif) == true
        let size = (try? file.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0
        guard heic || (size > 1_572_864 && type?.conforms(to: .gif) != true) else { return file }
        guard let source = CGImageSourceCreateWithURL(file as CFURL, nil) else { return file }
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: 2560,
        ]
        guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary) else { return file }
        let base = file.deletingPathExtension().lastPathComponent
        let target = folder().appending(path: "\(base.isEmpty ? "photo" : base).jpg")
        guard let destination = CGImageDestinationCreateWithURL(target as CFURL, UTType.jpeg.identifier as CFString, 1, nil) else {
            return file
        }
        CGImageDestinationAddImage(destination, image, [kCGImageDestinationLossyCompressionQuality: 0.9] as CFDictionary)
        return CGImageDestinationFinalize(destination) ? target : file
    }
}

struct PickedMedia: Transferable {
    let url: URL

    static var transferRepresentation: some TransferRepresentation {
        FileRepresentation(importedContentType: .movie) { received in
            try PickedMedia(url: stage(received.file))
        }
        FileRepresentation(importedContentType: .image) { received in
            try PickedMedia(url: Staging.prepareImage(stage(received.file)))
        }
    }

    private static func stage(_ file: URL) throws -> URL {
        let target = Staging.folder().appending(path: file.lastPathComponent)
        try FileManager.default.copyItem(at: file, to: target)
        return target
    }
}

@MainActor
@Observable
final class VoiceRecorder {
    static let maxDuration: TimeInterval = 15 * 60

    private(set) var recording = false
    private(set) var elapsed: TimeInterval = 0
    private(set) var level: Float = 0
    private(set) var denied = false
    @ObservationIgnored private var recorder: AVAudioRecorder?
    @ObservationIgnored private var meter: Task<Void, Never>?

    func start() async {
        guard !recording else { return }
        guard await AVAudioApplication.requestRecordPermission() else {
            denied = true
            return
        }
        #if os(iOS)
        try? AVAudioSession.sharedInstance().setCategory(.playAndRecord, mode: .default, options: [.defaultToSpeaker])
        try? AVAudioSession.sharedInstance().setActive(true)
        #endif
        let file = Staging.folder().appending(path: "voice.m4a")
        let settings: [String: Any] = [
            AVFormatIDKey: kAudioFormatMPEG4AAC,
            AVSampleRateKey: 44_100,
            AVNumberOfChannelsKey: 1,
            AVEncoderAudioQualityKey: AVAudioQuality.high.rawValue,
        ]
        guard let recorder = try? AVAudioRecorder(url: file, settings: settings) else { return }
        recorder.isMeteringEnabled = true
        guard recorder.record(forDuration: Self.maxDuration) else { return }
        self.recorder = recorder
        recording = true
        elapsed = 0
        meter = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(100))
                guard let self, let recorder = self.recorder else { return }
                recorder.updateMeters()
                elapsed = recorder.currentTime
                level = max(0, min(1, (recorder.averagePower(forChannel: 0) + 50) / 50))
            }
        }
    }

    func finish() -> (URL, TimeInterval)? {
        guard let recorder else { return nil }
        let duration = recorder.currentTime
        let url = recorder.url
        stop()
        return duration >= 0.5 ? (url, duration) : nil
    }

    func cancel() {
        let url = recorder?.url
        stop()
        if let url { try? FileManager.default.removeItem(at: url) }
    }

    private func stop() {
        meter?.cancel()
        meter = nil
        recorder?.stop()
        recorder = nil
        recording = false
        level = 0
        #if os(iOS)
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        #endif
    }
}
