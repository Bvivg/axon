import { ContentKind, contentOf, isDeleted, type AlbumItem } from "@/lib/chat/content";
import type { WireMessage } from "@/lib/ws/protocol";

export interface ViewerSlide {
  key: string;
  messageID: string;
  item: number;
  kind: ContentKind.Image | ContentKind.Video;
  url: string;
  preview: string;
  width: number;
  height: number;
  caption: string;
}

function slideOf(message: WireMessage, item: number, media: AlbumItem, caption: string): ViewerSlide {
  const common = { key: `${message.id}:${item}`, messageID: message.id, item, caption };
  return media.kind === ContentKind.Image
    ? {
        ...common,
        kind: ContentKind.Image,
        url: media.image.url,
        preview: media.image.thumbnailUrl,
        width: media.image.width,
        height: media.image.height,
      }
    : {
        ...common,
        kind: ContentKind.Video,
        url: media.video.url,
        preview: media.video.posterUrl,
        width: media.video.width,
        height: media.video.height,
      };
}

export function viewerSlidesOf(messages: readonly WireMessage[]): ViewerSlide[] {
  const slides: ViewerSlide[] = [];
  for (const message of messages) {
    if (isDeleted(message)) continue;
    const content = contentOf(message);
    const caption = message.body.trim();
    if (content.kind === ContentKind.Album) {
      content.items.forEach((media, item) => slides.push(slideOf(message, item, media, caption)));
    } else if (content.kind === ContentKind.Image || content.kind === ContentKind.Video) {
      slides.push(slideOf(message, 0, content, caption));
    }
  }
  return slides;
}

export function slideIndexOf(slides: readonly ViewerSlide[], messageID: string, item: number): number {
  return slides.findIndex((slide) => slide.messageID === messageID && slide.item === item);
}
