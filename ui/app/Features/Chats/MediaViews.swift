import AVFoundation
import AVKit
import ImageIO
import QuickLook
import SwiftUI
#if os(iOS)
import UIKit
#else
import AppKit
#endif

extension EnvironmentValues {
    @Entry var media: (any MediaAPI)?
}

enum Clipboard {
    static func copy(_ text: String) {
        #if os(iOS)
        UIPasteboard.general.string = text
        #else
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        #endif
    }
}

enum ImageDecoder {
    static func decode(_ data: Data, maxPixel: CGFloat) -> CGImage? {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil) else { return nil }
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixel,
        ]
        return CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary)
    }
}

struct AuthImage: View {
    let raw: String
    var maxPixel: CGFloat = 900
    var fit = false
    @Environment(\.media) private var media
    @State private var image: CGImage?
    @State private var failed = false

    var body: some View {
        ZStack {
            if let image {
                if fit {
                    Image(decorative: image, scale: 1).resizable().scaledToFit()
                } else {
                    Image(decorative: image, scale: 1).resizable().scaledToFill()
                }
            } else if failed {
                Image(systemName: "photo")
                    .font(.system(size: 22))
                    .foregroundStyle(Theme.muted)
            } else {
                ProgressView().controlSize(.small)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .task(id: raw) { await load() }
    }

    private func load() async {
        guard let media, let link = media.link(raw) else {
            failed = true
            return
        }
        do {
            let data = try await media.data(link)
            let pixels = maxPixel
            image = await Task.detached { ImageDecoder.decode(data, maxPixel: pixels) }.value
            failed = image == nil
        } catch {
            failed = true
        }
    }
}

struct FileContent: View {
    let name: String
    let size: Int64
    let raw: String
    @Environment(\.media) private var media
    @State private var loading = false
    @State private var preview: URL?
    @State private var failed = false

    var body: some View {
        Button {
            Task { await open() }
        } label: {
            HStack(spacing: 10) {
                ZStack {
                    RoundedRectangle(cornerRadius: 10, style: .continuous).fill(Theme.accent)
                    if loading {
                        ProgressView().controlSize(.small)
                    } else {
                        Image(systemName: failed ? "exclamationmark.triangle" : "doc")
                            .font(.system(size: 16, weight: .medium))
                            .foregroundStyle(Theme.primary)
                    }
                }
                .frame(width: 40, height: 40)
                VStack(alignment: .leading, spacing: 2) {
                    Text(name.isEmpty ? "File" : name)
                        .font(.geist(14, .medium, relativeTo: .subheadline))
                        .foregroundStyle(Theme.foreground)
                        .lineLimit(2)
                        .truncationMode(.middle)
                    Text(ChatFormat.size(size))
                        .font(.axCaption)
                        .foregroundStyle(Theme.muted)
                }
                Spacer(minLength: 0)
            }
            .frame(minWidth: 180, maxWidth: 260, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .quickLookPreview($preview)
        .accessibilityLabel("File \(name)")
    }

    private func open() async {
        guard !loading, let media, let link = media.link(raw) else { return }
        loading = true
        defer { loading = false }
        do {
            preview = try await media.download(link, named: name)
            failed = false
        } catch {
            failed = true
        }
    }
}

struct MediaItem: Identifiable, Hashable {
    let id: String
    let video: Bool
    let url: String
    let thumbnail: String
    let width: Int32
    let height: Int32
    let duration: Int64

    var aspect: CGFloat {
        guard width > 0, height > 0 else { return 1 }
        return min(2, max(0.5, CGFloat(width) / CGFloat(height)))
    }

    static func items(of message: ChatMessage) -> [MediaItem] {
        switch message.content {
        case .image(let image):
            [MediaItem(id: message.id, video: false, url: image.url, thumbnail: image.thumbnailURL,
                       width: image.width, height: image.height, duration: 0)]
        case .video(let video):
            [MediaItem(id: message.id, video: true, url: video.url, thumbnail: video.posterURL,
                       width: video.width, height: video.height, duration: video.durationMs)]
        case .album(let items):
            items.enumerated().compactMap { index, item in
                guard item.kind == .image || item.kind == .video else { return nil }
                return MediaItem(
                    id: "\(message.id)#\(index)", video: item.kind == .video, url: item.url,
                    thumbnail: item.kind == .video ? item.posterURL : item.thumbnailURL,
                    width: item.width, height: item.height, duration: item.durationMs
                )
            }
        default:
            []
        }
    }
}

struct MediaTile: View {
    let item: MediaItem

    var body: some View {
        AuthImage(raw: item.thumbnail.isEmpty ? item.url : item.thumbnail)
            .background(Theme.secondary)
            .overlay(alignment: .bottomLeading) {
                if item.video {
                    Label(Clock.duration(milliseconds: item.duration), systemImage: "play.fill")
                        .font(.axMono(11, medium: true, relativeTo: .caption2))
                        .foregroundStyle(.white)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 3)
                        .background(.black.opacity(0.55), in: .capsule)
                        .padding(6)
                }
            }
            .clipped()
            .contentShape(Rectangle())
    }
}

struct MediaViewer: View {
    let items: [MediaItem]
    @State var selection: String
    @Environment(\.dismiss) private var dismiss
    @Environment(\.media) private var media
    @State private var shared: URL?

    var body: some View {
        NavigationStack {
            TabView(selection: $selection) {
                ForEach(items) { item in
                    Group {
                        if item.video {
                            AuthVideo(raw: item.url)
                        } else {
                            ZoomableImage(raw: item.url)
                        }
                    }
                    .tag(item.id)
                }
            }
            #if os(iOS)
            .tabViewStyle(.page(indexDisplayMode: items.count > 1 ? .always : .never))
            #endif
            .background(Color.black)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Close", systemImage: "xmark") { dismiss() }
                }
                ToolbarItem(placement: .primaryAction) {
                    if let shared {
                        ShareLink(item: shared)
                    } else {
                        Button("Prepare to share", systemImage: "square.and.arrow.up") {
                            Task { await prepareShare() }
                        }
                    }
                }
            }
            .onChange(of: selection) { shared = nil }
        }
    }

    private func prepareShare() async {
        guard let item = items.first(where: { $0.id == selection }), let media, let link = media.link(item.url) else { return }
        shared = try? await media.download(link, named: link.lastPathComponent)
    }
}

struct ZoomableImage: View {
    let raw: String
    @State private var scale: CGFloat = 1
    @GestureState private var pinch: CGFloat = 1

    var body: some View {
        AuthImage(raw: raw, maxPixel: 2560, fit: true)
            .scaleEffect(scale * pinch)
            .gesture(
                MagnifyGesture()
                    .updating($pinch) { value, state, _ in state = value.magnification }
                    .onEnded { value in scale = min(4, max(1, scale * value.magnification)) }
            )
            .onTapGesture(count: 2) {
                withAnimation(.snappy) { scale = scale > 1 ? 1 : 2.5 }
            }
    }
}

struct AuthVideo: View {
    let raw: String
    @Environment(\.media) private var media
    @State private var player: AVPlayer?

    var body: some View {
        ZStack {
            if let player {
                VideoPlayer(player: player)
            } else {
                ProgressView().tint(.white)
            }
        }
        .task(id: raw) {
            guard let media, let link = media.link(raw) else { return }
            let headers = await media.playbackHeaders(for: link)
            let asset = AVURLAsset(url: link, options: ["AVURLAssetHTTPHeaderFieldsKey": headers])
            AudioPlayback.shared.stop()
            player = AVPlayer(playerItem: AVPlayerItem(asset: asset))
        }
        .onDisappear { player?.pause() }
    }
}
