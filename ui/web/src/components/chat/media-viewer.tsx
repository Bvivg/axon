"use client";

import dynamic from "next/dynamic";
import { useMemo, type CSSProperties } from "react";
import Lightbox, { stopNavigationEventsPropagation, type Slide } from "yet-another-react-lightbox";
import Captions from "yet-another-react-lightbox/plugins/captions";
import Counter from "yet-another-react-lightbox/plugins/counter";
import Download from "yet-another-react-lightbox/plugins/download";
import Zoom from "yet-another-react-lightbox/plugins/zoom";
import "yet-another-react-lightbox/styles.css";
import "yet-another-react-lightbox/plugins/captions.css";
import "yet-another-react-lightbox/plugins/counter.css";

import { ContentKind } from "@/lib/chat/content";
import type { ViewerSlide } from "@/lib/chat/viewer";

const VideoPlayer = dynamic(() => import("@/components/chat/video-player").then((m) => m.VideoPlayer), {
  ssr: false,
});

const maxZoom = 4;
const widescreen = 16 / 9;
const playerChrome = "48px";

declare module "yet-another-react-lightbox" {
  interface ChatVideoSlide extends GenericSlide {
    type: "chat-video";
    video: ViewerSlide;
  }

  interface SlideTypes {
    "chat-video": ChatVideoSlide;
  }
}

function videoFrame(slide: ViewerSlide, rect: { width: number; height: number }): CSSProperties {
  const ratio = slide.width > 0 && slide.height > 0 ? slide.width / slide.height : widescreen;
  return {
    aspectRatio: String(ratio),
    width: `min(${rect.width}px, calc((${rect.height}px - ${playerChrome}) * ${ratio}))`,
  };
}

export function MediaViewer({
  slides,
  index,
  onClose,
}: {
  slides: ViewerSlide[];
  index: number | null;
  onClose: () => void;
}) {
  const items = useMemo<Slide[]>(
    () =>
      slides.map((slide): Slide => {
        const common = { description: slide.caption || undefined, download: slide.url };
        return slide.kind === ContentKind.Video
          ? { type: "chat-video", video: slide, ...common }
          : {
              src: slide.url,
              alt: slide.caption || "Photo",
              width: slide.width || undefined,
              height: slide.height || undefined,
              ...common,
            };
      }),
    [slides],
  );

  return (
    <Lightbox
      open={index !== null && index >= 0}
      index={index ?? 0}
      close={onClose}
      slides={items}
      plugins={[Captions, Counter, Download, Zoom]}
      carousel={{ finite: true }}
      zoom={{ maxZoomPixelRatio: maxZoom, scrollToZoom: true }}
      captions={{ descriptionTextAlign: "center", descriptionMaxLines: 4 }}
      controller={{ closeOnBackdropClick: true }}
      render={{
        slide: ({ slide, rect, offset }) =>
          slide.type === "chat-video" ? (
            <div {...stopNavigationEventsPropagation()}>
              <VideoPlayer
                key={slide.video.key}
                src={slide.video.url}
                poster={slide.video.preview}
                active={offset === 0}
                style={videoFrame(slide.video, rect)}
              />
            </div>
          ) : undefined,
      }}
    />
  );
}
