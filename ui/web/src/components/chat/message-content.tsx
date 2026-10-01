"use client";

import { Download, FileText, Pause, Play } from "lucide-react";
import Image from "next/image";
import { useEffect, useRef, useState, type ChangeEvent } from "react";

import {
  ContentKind,
  type AlbumItem,
  type Content,
  type FileContent,
  type ImageContent,
  type VideoContent,
  type VoiceContent,
} from "@/lib/chat/content";
import { formatBytes, formatDuration } from "@/lib/format";
import { cn } from "@/lib/utils";

const maxMediaWidth = 280;
const maxMediaHeight = 320;
const minMediaWidth = 140;

export function mediaBoxSize(width: number, height: number): { width: number; height: number } {
  if (width <= 0 || height <= 0) {
    return { width: maxMediaWidth, height: Math.round(maxMediaWidth * 0.75) };
  }
  const scale = Math.min(1, maxMediaWidth / width, maxMediaHeight / height);
  const w = Math.max(minMediaWidth, Math.round(width * scale));
  return { width: w, height: Math.round((w * height) / width) };
}

export function MessageMedia({
  content,
  caption,
  mine,
  onOpen,
  picked,
  onTogglePicked,
}: {
  content: Content;
  caption: string;
  mine: boolean;
  onOpen: (item: number) => void;
  picked?: (item: number) => boolean;
  onTogglePicked?: (item: number) => void;
}) {
  switch (content.kind) {
    case ContentKind.Album:
      return (
        <AlbumGrid
          items={content.items}
          caption={caption}
          onOpen={onOpen}
          picked={picked}
          onTogglePicked={onTogglePicked}
        />
      );
    case ContentKind.Image:
      return <PhotoTile image={content.image} caption={caption} onOpen={() => onOpen(0)} />;
    case ContentKind.Video:
      return <VideoTile video={content.video} onOpen={() => onOpen(0)} />;
    case ContentKind.Voice:
      return <VoicePlayer voice={content.voice} mine={mine} />;
    case ContentKind.File:
      return <FileCard file={content.file} mine={mine} />;
    default:
      return null;
  }
}

const albumRows: Record<number, number[]> = {
  2: [2],
  3: [1, 2],
  4: [2, 2],
  5: [2, 3],
  6: [3, 3],
  7: [2, 2, 3],
  8: [2, 3, 3],
  9: [3, 3, 3],
  10: [2, 2, 3, 3],
};

const ratioOf = (item: AlbumItem) => {
  const { width, height } = item.kind === ContentKind.Image ? item.image : item.video;
  return width > 0 && height > 0 ? Math.min(2, Math.max(0.5, width / height)) : 1;
};

export function AlbumGrid({
  items,
  caption,
  onOpen,
  picked,
  onTogglePicked,
}: {
  items: AlbumItem[];
  caption: string;
  onOpen: (item: number) => void;
  picked?: (item: number) => boolean;
  onTogglePicked?: (item: number) => void;
}) {
  const rows = albumRows[items.length] ?? [items.length];
  const starts = rows.map((_, row) => rows.slice(0, row).reduce((sum, count) => sum + count, 0));
  return (
    <div role="group" aria-label={`Album of ${items.length}`} className="flex w-[280px] max-w-full flex-col gap-0.5 overflow-hidden rounded-xl">
      {rows.map((count, row) => {
        const start = starts[row];
        return (
          <div key={row} className="flex gap-0.5">
            {items.slice(start, start + count).map((item, offset) => {
              const index = start + offset;
              const ratio = ratioOf(item);
              const thumb = item.kind === ContentKind.Image ? item.image.thumbnailUrl : item.video.posterUrl;
              const selecting = onTogglePicked !== undefined;
              return (
                <button
                  key={index}
                  type="button"
                  role={selecting ? "checkbox" : undefined}
                  aria-checked={selecting ? (picked?.(index) ?? false) : undefined}
                  aria-label={
                    selecting
                      ? `Select item ${index + 1} of the album`
                      : item.kind === ContentKind.Image
                        ? `Open photo ${index + 1} of ${items.length}`
                        : `Play video ${index + 1} of ${items.length}`
                  }
                  onClick={(event) => {
                    if (!selecting) return onOpen(index);
                    event.stopPropagation();
                    onTogglePicked(index);
                  }}
                  className="relative min-w-0 overflow-hidden bg-muted"
                  style={{ flexGrow: ratio, flexBasis: 0, aspectRatio: String(ratio) }}
                >
                  {thumb ? (
                    <Image src={thumb} alt={caption || ""} fill unoptimized sizes="140px" className="object-cover" />
                  ) : null}
                  {item.kind === ContentKind.Video ? (
                    <span className="absolute bottom-1 left-1 flex items-center gap-1 rounded-md bg-black/55 px-1.5 py-0.5 font-mono text-[10px] text-white">
                      <Play aria-hidden className="size-2.5 fill-current" />
                      {formatDuration(item.video.durationMs)}
                    </span>
                  ) : null}
                  {selecting ? (
                    <span
                      aria-hidden
                      className={cn(
                        "absolute top-1.5 right-1.5 size-5 rounded-full border-2 border-white shadow",
                        picked?.(index) ? "bg-signal" : "bg-black/25",
                      )}
                    />
                  ) : null}
                </button>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}

export function PhotoTile({
  image,
  caption,
  onOpen,
  className,
}: {
  image: ImageContent;
  caption: string;
  onOpen: () => void;
  className?: string;
}) {
  const box = mediaBoxSize(image.width, image.height);
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label={caption ? `Open photo: ${caption}` : "Open photo"}
      className={cn("relative block max-w-full overflow-hidden rounded-xl bg-muted", className)}
      style={{ width: box.width, aspectRatio: `${box.width} / ${box.height}` }}
    >
      <Image
        src={image.thumbnailUrl}
        alt={caption || "Photo"}
        fill
        unoptimized
        sizes={`${box.width}px`}
        className="object-cover"
      />
    </button>
  );
}

export function VideoTile({
  video,
  onOpen,
  className,
}: {
  video: VideoContent;
  onOpen: () => void;
  className?: string;
}) {
  const box = mediaBoxSize(video.width, video.height);
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label="Play video"
      className={cn("relative block max-w-full overflow-hidden rounded-xl bg-black", className)}
      style={{ width: box.width, aspectRatio: `${box.width} / ${box.height}` }}
    >
      {video.posterUrl ? (
        <Image src={video.posterUrl} alt="" fill unoptimized sizes={`${box.width}px`} className="object-cover" />
      ) : null}
      <span className="absolute inset-0 flex items-center justify-center">
        <span className="flex size-12 items-center justify-center rounded-full bg-black/55 text-white backdrop-blur-sm">
          <Play aria-hidden className="size-5 translate-x-px fill-current" />
        </span>
      </span>
      <span className="absolute bottom-1.5 left-1.5 rounded-md bg-black/55 px-1.5 py-0.5 font-mono text-[10px] text-white">
        {formatDuration(video.durationMs)}
      </span>
    </button>
  );
}

let playing: HTMLAudioElement | null = null;

export function VoicePlayer({ voice, mine }: { voice: VoiceContent; mine: boolean }) {
  const audio = useRef<HTMLAudioElement>(null);
  const [isPlaying, setIsPlaying] = useState(false);
  const [positionMs, setPositionMs] = useState(0);

  useEffect(() => {
    const element = audio.current;
    return () => {
      if (playing === element) {
        playing = null;
      }
    };
  }, []);

  const toggle = () => {
    const element = audio.current;
    if (!element) return;
    if (element.paused) {
      if (playing && playing !== element) {
        playing.pause();
      }
      playing = element;
      void element.play();
    } else {
      element.pause();
    }
  };

  const seek = (event: ChangeEvent<HTMLInputElement>) => {
    const element = audio.current;
    const next = Number(event.target.value);
    setPositionMs(next);
    if (element) {
      element.currentTime = next / 1000;
    }
  };

  const shown = isPlaying || positionMs > 0 ? positionMs : voice.durationMs;

  return (
    <div className="flex w-60 max-w-full items-center gap-2.5 py-0.5">
      <button
        type="button"
        onClick={toggle}
        aria-label={isPlaying ? "Pause voice message" : "Play voice message"}
        className={cn(
          "flex size-9 shrink-0 items-center justify-center rounded-full transition-colors",
          mine ? "bg-signal-foreground/20 text-signal-foreground" : "bg-signal text-signal-foreground",
        )}
      >
        {isPlaying ? (
          <Pause aria-hidden className="size-4 fill-current" />
        ) : (
          <Play aria-hidden className="size-4 translate-x-px fill-current" />
        )}
      </button>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <input
          type="range"
          min={0}
          max={Math.max(voice.durationMs, 1)}
          step={100}
          value={Math.min(positionMs, voice.durationMs)}
          onChange={seek}
          aria-label="Voice message position"
          className={cn("h-1 w-full cursor-pointer", mine ? "accent-signal-foreground" : "accent-signal")}
        />
        <span className={cn("font-mono text-[10px]", mine ? "text-signal-foreground/80" : "text-muted-foreground")}>
          {formatDuration(shown)}
        </span>
      </div>
      <audio
        ref={audio}
        src={voice.url}
        preload="none"
        onPlay={() => setIsPlaying(true)}
        onPause={() => setIsPlaying(false)}
        onEnded={() => {
          setIsPlaying(false);
          setPositionMs(0);
        }}
        onTimeUpdate={(event) => setPositionMs(event.currentTarget.currentTime * 1000)}
      />
    </div>
  );
}

export function FileCard({ file, mine, className }: { file: FileContent; mine: boolean; className?: string }) {
  return (
    <a
      href={file.url}
      download={file.filename}
      target="_blank"
      rel="noopener noreferrer"
      aria-label={`Download ${file.filename}`}
      className={cn("flex w-64 max-w-full items-center gap-3 rounded-lg py-0.5", className)}
    >
      <span
        className={cn(
          "flex size-10 shrink-0 items-center justify-center rounded-lg",
          mine ? "bg-signal-foreground/20" : "bg-card",
        )}
      >
        <FileText aria-hidden className="size-5" strokeWidth={1.5} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{file.filename}</span>
        <span className={cn("block text-xs", mine ? "text-signal-foreground/75" : "text-muted-foreground")}>
          {formatBytes(file.sizeBytes)}
        </span>
      </span>
      <Download aria-hidden className="size-4 shrink-0 opacity-70" />
    </a>
  );
}
