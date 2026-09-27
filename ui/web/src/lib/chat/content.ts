import type { WireMessage } from "@/lib/ws/protocol";
import { formatDuration } from "@/lib/format";

export enum ContentKind {
  Text = "text",
  Image = "image",
  Video = "video",
  Voice = "voice",
  File = "attachment",
  System = "system",
}

export interface ImageContent {
  url: string;
  thumbnailUrl: string;
  width: number;
  height: number;
  sizeBytes: number;
  mime: string;
}

export interface VideoContent {
  url: string;
  posterUrl: string;
  width: number;
  height: number;
  durationMs: number;
  sizeBytes: number;
  mime: string;
}

export interface VoiceContent {
  url: string;
  durationMs: number;
  mime: string;
}

export interface FileContent {
  url: string;
  filename: string;
  mime: string;
  sizeBytes: number;
}

export type Content =
  | { kind: ContentKind.Text }
  | { kind: ContentKind.Image; image: ImageContent }
  | { kind: ContentKind.Video; video: VideoContent }
  | { kind: ContentKind.Voice; voice: VoiceContent }
  | { kind: ContentKind.File; file: FileContent }
  | { kind: ContentKind.System };

const text = (payload: Record<string, unknown>, key: string): string =>
  typeof payload[key] === "string" ? (payload[key] as string) : "";

const number = (payload: Record<string, unknown>, key: string): number =>
  typeof payload[key] === "number" ? (payload[key] as number) : Number(payload[key] ?? 0) || 0;

export function contentOf(message: WireMessage): Content {
  const payload = message.payload ?? {};

  switch (message.kind) {
    case ContentKind.Image: {
      const url = text(payload, "url");
      if (!url) break;
      return {
        kind: ContentKind.Image,
        image: {
          url,
          thumbnailUrl: text(payload, "thumbnail_url") || url,
          width: number(payload, "width"),
          height: number(payload, "height"),
          sizeBytes: number(payload, "size_bytes"),
          mime: text(payload, "mime"),
        },
      };
    }
    case ContentKind.Video: {
      const url = text(payload, "url");
      if (!url) break;
      return {
        kind: ContentKind.Video,
        video: {
          url,
          posterUrl: text(payload, "poster_url"),
          width: number(payload, "width"),
          height: number(payload, "height"),
          durationMs: number(payload, "duration_ms"),
          sizeBytes: number(payload, "size_bytes"),
          mime: text(payload, "mime"),
        },
      };
    }
    case ContentKind.Voice: {
      const url = text(payload, "url");
      if (!url) break;
      return {
        kind: ContentKind.Voice,
        voice: { url, durationMs: number(payload, "duration_ms"), mime: text(payload, "mime") },
      };
    }
    case ContentKind.File: {
      const url = text(payload, "url");
      if (!url) break;
      return {
        kind: ContentKind.File,
        file: {
          url,
          filename: text(payload, "filename") || "file",
          mime: text(payload, "mime"),
          sizeBytes: number(payload, "size_bytes"),
        },
      };
    }
    case ContentKind.System:
      return { kind: ContentKind.System };
  }

  return { kind: ContentKind.Text };
}

export function previewOf(message: WireMessage): string {
  const content = contentOf(message);
  const caption = message.body.trim();

  switch (content.kind) {
    case ContentKind.Image:
      return caption ? `📷 ${caption}` : "📷 Photo";
    case ContentKind.Video:
      return caption ? `🎬 ${caption}` : "🎬 Video";
    case ContentKind.Voice:
      return `🎤 Voice message · ${formatDuration(content.voice.durationMs)}`;
    case ContentKind.File:
      return `📎 ${content.file.filename}`;
    case ContentKind.System:
      return caption || "System message";
    default:
      return message.body;
  }
}
