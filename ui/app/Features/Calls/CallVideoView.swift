import AVKit
@preconcurrency import LiveKit
import SwiftUI
@preconcurrency import WebRTC

struct CallVideoView: View {
    let track: AnyObject
    var mirrored = false
    var fill = true

    var body: some View {
        if let track = track as? VideoTrack {
            SwiftUIVideoView(track, layoutMode: fill ? .fill : .fit, mirrorMode: mirrored ? .mirror : .auto)
        } else if let track = track as? RTCVideoTrack {
            RTCVideoSurface(track: track, mirrored: mirrored, fill: fill)
        }
    }
}

#if os(iOS)
struct RTCVideoSurface: UIViewRepresentable {
    let track: RTCVideoTrack
    var mirrored = false
    var fill = true

    func makeUIView(context: Context) -> RTCMTLVideoView {
        let view = RTCMTLVideoView()
        view.videoContentMode = fill ? .scaleAspectFill : .scaleAspectFit
        view.transform = mirrored ? CGAffineTransform(scaleX: -1, y: 1) : .identity
        view.clipsToBounds = true
        track.add(view)
        context.coordinator.track = track
        return view
    }

    func updateUIView(_ view: RTCMTLVideoView, context: Context) {
        view.videoContentMode = fill ? .scaleAspectFill : .scaleAspectFit
        view.transform = mirrored ? CGAffineTransform(scaleX: -1, y: 1) : .identity
        guard context.coordinator.track !== track else { return }
        context.coordinator.track?.remove(view)
        track.add(view)
        context.coordinator.track = track
    }

    static func dismantleUIView(_ view: RTCMTLVideoView, coordinator: Coordinator) {
        coordinator.track?.remove(view)
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    final class Coordinator {
        var track: RTCVideoTrack?
    }
}
#else
struct RTCVideoSurface: NSViewRepresentable {
    let track: RTCVideoTrack
    var mirrored = false
    var fill = true

    func makeNSView(context: Context) -> RTCMTLNSVideoView {
        let view = RTCMTLNSVideoView()
        track.add(view)
        context.coordinator.track = track
        return view
    }

    func updateNSView(_ view: RTCMTLNSVideoView, context: Context) {
        guard context.coordinator.track !== track else { return }
        context.coordinator.track?.remove(view)
        track.add(view)
        context.coordinator.track = track
    }

    static func dismantleNSView(_ view: RTCMTLNSVideoView, coordinator: Coordinator) {
        coordinator.track?.remove(view)
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    final class Coordinator {
        var track: RTCVideoTrack?
    }
}
#endif

#if os(iOS)
struct CallPictureInPicture: UIViewRepresentable {
    let track: RTCVideoTrack?

    func makeUIView(context: Context) -> UIView {
        let view = UIView()
        view.isUserInteractionEnabled = false
        context.coordinator.attach(to: view)
        context.coordinator.show(track)
        return view
    }

    func updateUIView(_ view: UIView, context: Context) {
        context.coordinator.show(track)
    }

    static func dismantleUIView(_ view: UIView, coordinator: Coordinator) {
        coordinator.show(nil)
        coordinator.controller?.stopPictureInPicture()
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    @MainActor
    final class Coordinator {
        var controller: AVPictureInPictureController?
        private let content = AVPictureInPictureVideoCallViewController()
        private let surface = RTCMTLVideoView()
        private var track: RTCVideoTrack?

        func attach(to source: UIView) {
            guard AVPictureInPictureController.isPictureInPictureSupported() else { return }
            surface.videoContentMode = .scaleAspectFill
            surface.translatesAutoresizingMaskIntoConstraints = false
            content.view.addSubview(surface)
            NSLayoutConstraint.activate([
                surface.leadingAnchor.constraint(equalTo: content.view.leadingAnchor),
                surface.trailingAnchor.constraint(equalTo: content.view.trailingAnchor),
                surface.topAnchor.constraint(equalTo: content.view.topAnchor),
                surface.bottomAnchor.constraint(equalTo: content.view.bottomAnchor),
            ])
            content.preferredContentSize = CGSize(width: 9, height: 16)
            let source = AVPictureInPictureController.ContentSource(activeVideoCallSourceView: source, contentViewController: content)
            let controller = AVPictureInPictureController(contentSource: source)
            controller.canStartPictureInPictureAutomaticallyFromInline = true
            self.controller = controller
        }

        func show(_ next: RTCVideoTrack?) {
            guard track !== next else { return }
            track?.remove(surface)
            track = next
            next?.add(surface)
        }
    }
}
#endif
