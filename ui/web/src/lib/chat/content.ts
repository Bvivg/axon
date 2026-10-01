import type { WireMessage, WireReplyPreview } from "@/lib/ws/protocol";
import { formatDuration } from "@/lib/format";

export enum ContentKind {
  Text = "text",
  Image = "image",
  Video = "video",
  Voice = "voice",
  File = "attachment",
  System = "system",
}

export enum SystemEvent {
  GroupCreated = "group_created",
  MemberAdded = "member_added",
  MemberRemoved = "member_removed",
  MemberLeft = "member_left",
  GroupRenamed = "group_renamed",
}

export type NameOf = (userID: string | undefined) => string;

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

export function isDeleted(message: WireMessage): boolean {
  return !!message.deleted_at;
}

export function isEditable(message: WireMessage): boolean {
  if (isDeleted(message) || message.forwarded_from_id) {
    return false;
  }
  const kind = contentOf(message).kind;
  return (
    kind === ContentKind.Text || kind === ContentKind.Image || kind === ContentKind.Video || kind === ContentKind.File
  );
}

export function targetIDsOf(payload: Record<string, unknown>): string[] {
  const many = Array.isArray(payload.target_ids)
    ? payload.target_ids.filter((id): id is string => typeof id === "string")
    : [];
  if (many.length > 0) {
    return many;
  }
  const one = text(payload, "target_id");
  return one ? [one] : [];
}

function listOf(names: string[]): string {
  if (names.length <= 1) {
    return names[0] ?? "Someone";
  }
  if (names.length <= 3) {
    return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
  }
  return `${names.slice(0, 2).join(", ")} and ${names.length - 2} others`;
}

export function systemTextOf(message: WireMessage, nameOf: NameOf, selfID?: string): string {
  const payload = message.payload ?? {};
  const actor = nameOf(text(payload, "actor_id") || undefined);
  const target = listOf(targetIDsOf(payload).map((id) => (id === selfID ? "you" : nameOf(id))));
  const name = message.body.trim();

  switch (text(payload, "event")) {
    case SystemEvent.GroupCreated:
      return `${actor} created the group “${name}”`;
    case SystemEvent.MemberAdded:
      return `${actor} added ${target}`;
    case SystemEvent.MemberRemoved:
      return `${actor} removed ${target}`;
    case SystemEvent.MemberLeft:
      return `${actor} left the group`;
    case SystemEvent.GroupRenamed:
      return `${actor} renamed the group to “${name}”`;
    default:
      return name || "The chat changed";
  }
}

function systemSummaryOf(message: WireMessage): string {
  const name = message.body.trim();
  const payload = message.payload ?? {};
  switch (text(payload, "event")) {
    case SystemEvent.GroupCreated:
      return "Group created";
    case SystemEvent.MemberAdded:
      return targetIDsOf(payload).length > 1 ? "New members joined" : "New member joined";
    case SystemEvent.MemberRemoved:
      return "A member was removed";
    case SystemEvent.MemberLeft:
      return "A member left";
    case SystemEvent.GroupRenamed:
      return `Renamed to “${name}”`;
    default:
      return name || "The chat changed";
  }
}

export function membersLabel(count: number): string {
  return `${count} ${count === 1 ? "member" : "members"}`;
}

export function quoteOf(preview: WireReplyPreview): string {
  if (preview.deleted) {
    return "Deleted message";
  }
  const caption = preview.body.trim();
  switch (preview.kind) {
    case ContentKind.Image:
      return caption || "Photo";
    case ContentKind.Video:
      return caption || "Video";
    case ContentKind.Voice:
      return "Voice message";
    case ContentKind.File:
      return caption || "File";
    default:
      return caption;
  }
}

export function previewOf(message: WireMessage): string {
  if (isDeleted(message)) {
    return "Message deleted";
  }

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
      return systemSummaryOf(message);
    default:
      return message.body;
  }
}
