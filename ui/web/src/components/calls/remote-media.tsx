"use client";

import { PictureInPicture2 } from "lucide-react";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { Button } from "@/components/ui/button";
import { closeCallPiP, isCallPiP, openCallPiP, supportsCallPiP } from "@/components/calls/call-pip";

export function CallVideo({ track, label, allowPiP = true }: { track: MediaStreamTrack; label: string; allowPiP?: boolean }) {
  const element = useRef<HTMLVideoElement>(null);
  const [supported, setSupported] = useState(false);
  const [open, setOpen] = useState(false);
  const [error, setError] = useState(false);
  useEffect(() => {
    const video = element.current;
    if (!video) return;
    video.srcObject = new MediaStream([track]);
    const update = () => {
      setSupported(supportsCallPiP(video));
      setOpen(isCallPiP(video));
    };
    const events = ["loadedmetadata", "playing", "enterpictureinpicture", "leavepictureinpicture", "webkitpresentationmodechanged"];
    for (const event of events) video.addEventListener(event, update);
    return () => {
      for (const event of events) video.removeEventListener(event, update);
      void closeCallPiP(video).catch(() => undefined);
      video.pause();
      video.srcObject = null;
    };
  }, [track]);

  async function toggle() {
    const video = element.current;
    if (!video || track.readyState !== "live") return;
    setError(false);
    try {
      if (isCallPiP(video)) await closeCallPiP(video);
      else {
        await openCallPiP(video);
        if (element.current !== video || track.readyState !== "live" || video.srcObject === null) await closeCallPiP(video);
      }
    } catch {
      setError(true);
    }
  }

  return <>
    <video ref={element} aria-label={label} autoPlay muted playsInline className="size-full object-contain" />
    {allowPiP && supported ? <Button variant="outline" size="icon" className="absolute top-1 right-1 z-10 min-h-11 min-w-11" aria-label={open ? `Exit picture-in-picture for ${label}` : `Picture-in-picture for ${label}`} aria-pressed={open} onClick={() => { void toggle(); }}><PictureInPicture2 /></Button> : null}
    {error ? <span role="status" className="absolute bottom-1 left-1 rounded bg-background px-2 py-1 text-xs">Picture-in-picture unavailable</span> : null}
  </>;
}

export function useVideoAvailable(track: MediaStreamTrack | undefined) {
  return useSyncExternalStore(
    listener => {
      track?.addEventListener("mute", listener);
      track?.addEventListener("unmute", listener);
      track?.addEventListener("ended", listener);
      return () => {
        track?.removeEventListener("mute", listener);
        track?.removeEventListener("unmute", listener);
        track?.removeEventListener("ended", listener);
      };
    },
    () => !!track && track.readyState === "live" && !track.muted,
    () => false,
  );
}
