"use client";

import { Download, FileText, Pause, Play } from "lucide-react";
import Image from "next/image";
import { useEffect, useRef, useState, type ChangeEvent } from "react";

import {
  ContentKind,
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
}: {
  content: Content;
  caption: string;
  mine: boolean;
  onOpen: () => void;
}) {
  switch (content.kind) {
    case ContentKind.Image:
      return <PhotoTile image={content.image} caption={caption} onOpen={onOpen} />;
    case ContentKind.Video:
      return <VideoTile video={content.video} onOpen={onOpen} />;
    case ContentKind.Voice:
      return <VoicePlayer voice={content.voice} mine={mine} />;
    case ContentKind.File:
      return <FileCard file={content.file} mine={mine} />;
    default:
      return null;
  }
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
