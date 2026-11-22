import AVFoundation
import SwiftUI

@MainActor
@Observable
final class AudioPlayback {
    static let shared = AudioPlayback()

    private(set) var current: String?
    private(set) var playing = false
    private(set) var position: Double = 0
    private(set) var duration: Double = 0
    @ObservationIgnored private var player: AVPlayer?
    @ObservationIgnored private var observer: Any?
    @ObservationIgnored private var ended: NSObjectProtocol?

    func toggle(_ id: String, raw: String, media: (any MediaAPI)?, duration known: Double) async {
        if current == id, let player {
            if playing {
                player.pause()
                playing = false
            } else {
                player.play()
                playing = true
            }
            return
        }
        stop()
        guard let media, let link = media.link(raw) else { return }
        let headers = await media.playbackHeaders(for: link)
        let asset = AVURLAsset(url: link, options: ["AVURLAssetHTTPHeaderFieldsKey": headers])
        let player = AVPlayer(playerItem: AVPlayerItem(asset: asset))
        #if os(iOS)
        try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .spokenAudio)
        try? AVAudioSession.sharedInstance().setActive(true)
        #endif
        self.player = player
        current = id
        duration = known
        position = 0
        observer = player.addPeriodicTimeObserver(forInterval: CMTime(value: 1, timescale: 10), queue: .main) { [weak self] time in
            MainActor.assumeIsolated {
                self?.position = time.seconds
                if let total = player.currentItem?.duration.seconds, total.isFinite, total > 0 { self?.duration = total }
            }
        }
        ended = NotificationCenter.default.addObserver(
            forName: AVPlayerItem.didPlayToEndTimeNotification,
            object: player.currentItem,
            queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.stop() }
        }
        player.play()
        playing = true
    }

    func seek(_ id: String, to fraction: Double) {
        guard current == id, let player, duration > 0 else { return }
        player.seek(to: CMTime(seconds: duration * fraction, preferredTimescale: 600))
    }

    func stop() {
        player?.pause()
        if let observer { player?.removeTimeObserver(observer) }
        if let ended { NotificationCenter.default.removeObserver(ended) }
        observer = nil
        ended = nil
        player = nil
        current = nil
        playing = false
        position = 0
    }
}

struct VoiceContent: View {
    let id: String
    let voice: Axon_Chat_V1_VoicePayload
    let tint: Color
    @Environment(\.media) private var media
    private var playback: AudioPlayback { .shared }

    var body: some View {
        let active = playback.current == id
        let total = active && playback.duration > 0 ? playback.duration : Double(voice.durationMs) / 1000
        let fraction = active && total > 0 ? min(1, playback.position / total) : 0
        HStack(spacing: 10) {
            Button {
                Task { await playback.toggle(id, raw: voice.url, media: media, duration: Double(voice.durationMs) / 1000) }
            } label: {
                Image(systemName: active && playback.playing ? "pause.fill" : "play.fill")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Theme.onPrimary)
                    .frame(width: 36, height: 36)
                    .background(Theme.primary, in: .circle)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(active && playback.playing ? "Pause voice message" : "Play voice message")
            VStack(alignment: .leading, spacing: 4) {
                Waveform(seed: id, fraction: fraction, tint: tint)
                    .frame(height: 22)
                Text(Clock.duration(.seconds(active ? max(0, total - playback.position) : total)))
                    .font(.axMono(11, relativeTo: .caption2))
                    .foregroundStyle(Theme.muted)
            }
        }
        .frame(width: 210, alignment: .leading)
    }
}

struct Waveform: View {
    let seed: String
    let fraction: Double
    let tint: Color

    var body: some View {
        let bars = Self.bars(for: seed)
        GeometryReader { geometry in
            HStack(alignment: .center, spacing: 2) {
                ForEach(bars.indices, id: \.self) { index in
                    Capsule()
                        .fill(Double(index) / Double(bars.count) < fraction ? tint : Theme.muted.opacity(0.45))
                        .frame(width: max(1.5, (geometry.size.width - CGFloat(bars.count - 1) * 2) / CGFloat(bars.count)))
                        .frame(height: max(3, geometry.size.height * bars[index]))
                }
            }
            .frame(maxHeight: .infinity)
        }
    }

    static func bars(for seed: String, count: Int = 32) -> [CGFloat] {
        var state = seed.unicodeScalars.reduce(UInt64(1469598103934665603)) { ($0 ^ UInt64($1.value)) &* 1099511628211 }
        return (0..<count).map { _ in
            state = state &* 6364136223846793005 &+ 1442695040888963407
            return 0.25 + CGFloat(state >> 33) / CGFloat(UInt64(1) << 31) * 0.75
        }
    }
}
