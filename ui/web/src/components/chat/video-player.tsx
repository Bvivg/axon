"use client";

import MediaThemeMinimal from "@player.style/minimal/react";
import { useEffect, useRef, type CSSProperties } from "react";

import { cn } from "@/lib/utils";

const expectedRefusals = new Set(["NotAllowedError", "AbortError"]);

export function VideoPlayer({
  src,
  poster,
  active,
  className,
  style,
}: {
  src: string;
  poster?: string;
  active?: boolean;
  className?: string;
  style?: CSSProperties;
}) {
  const video = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    const element = video.current;
    if (!element || active === undefined) return;
    if (!active) {
      element.pause();
      return;
    }
    element.play().catch((error: unknown) => {
      if (error instanceof DOMException && expectedRefusals.has(error.name)) return;
      throw error;
    });
  }, [active]);

  return (
    <MediaThemeMinimal className={cn("block overflow-hidden bg-black", className)} style={style}>
      <video
        ref={video}
        slot="media"
        src={src}
        poster={poster || undefined}
        preload="metadata"
        playsInline
        aria-label="Video"
        className="size-full object-contain"
      />
    </MediaThemeMinimal>
  );
}
