"use client";

import { Maximize2, Minimize2, Shrink } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { useCallAudio } from "@/components/calls/call-audio-renderer";
import { CallControls, CallEndButton } from "@/components/calls/call-controls";
import { useCallDisplay } from "@/components/calls/call-display";
import { useCall } from "@/components/calls/call-provider";
import { CallStage } from "@/components/calls/call-stage";
import { Button } from "@/components/ui/button";
import type { Call } from "@/gen/axon/calls/v1/calls_pb";
import { cn } from "@/lib/utils";

export function CallShell({ call }: { call: Call }) {
  const { manager, view, expanded, minimize, shortcuts } = useCall();
  const { name, status, elapsed, profiles, active, migrating } = useCallDisplay();
  const audio = useCallAudio();
  const root = useRef<HTMLElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const ownsFullscreen = useRef(false);
  const [fullscreen, setFullscreen] = useState<"native" | "app" | null>(null);
  if (!expanded && fullscreen) setFullscreen(null);

  useEffect(() => {
    if (!expanded) {
      if (ownsFullscreen.current && document.fullscreenElement === document.documentElement) void document.exitFullscreen().catch(() => {});
      return;
    }
    heading.current?.focus({ preventScroll: true });
    function changed() {
      if (!ownsFullscreen.current) return;
      const entered = document.fullscreenElement === document.documentElement;
      if (!entered) ownsFullscreen.current = false;
      setFullscreen(entered ? "native" : null);
    }
    document.addEventListener("fullscreenchange", changed);
    return () => {
      document.removeEventListener("fullscreenchange", changed);
      if (ownsFullscreen.current && document.fullscreenElement === document.documentElement) {
        void document.exitFullscreen().catch(() => {});
      }
      ownsFullscreen.current = false;
    };
  }, [expanded]);

  function exitFullscreen() {
    if (fullscreen === "native") void document.exitFullscreen().catch(() => {});
    else setFullscreen(null);
  }

  function toggleFullscreen() {
    if (fullscreen) { exitFullscreen(); return; }
    const target = document.documentElement;
    if (!document.fullscreenEnabled || !target.requestFullscreen) {
      setFullscreen("app");
      return;
    }
    ownsFullscreen.current = true;
    void target.requestFullscreen().then(() => {
      if (!ownsFullscreen.current && document.fullscreenElement === target) void document.exitFullscreen().catch(() => {});
    }).catch(() => {
      if (!ownsFullscreen.current) return;
      ownsFullscreen.current = false;
      setFullscreen("app");
    });
  }

  function collapse() {
    setFullscreen(null);
    minimize();
    requestAnimationFrame(() => [...document.querySelectorAll<HTMLButtonElement>("[data-call-return]")].find(button => button.getClientRects().length)?.focus());
  }

  return (
    <section ref={root} role="dialog" aria-label="Call" aria-modal="false" data-call-shell data-fullscreen={fullscreen ?? "off"}
      hidden={!expanded}
      style={expanded ? undefined : { display: "none" }}
      className={cn("call-stage-frame min-w-0 overflow-hidden bg-background", fullscreen ? "fixed inset-0 z-50" : "absolute inset-0 z-30")}
      onKeyDown={event => {
        if (event.defaultPrevented || !(event.target instanceof HTMLElement) || !root.current?.contains(event.target)) return;
        if (event.target.closest("[role=menu],[role=dialog]:not([data-call-shell])")) return;
        if (shortcuts && !event.repeat && !event.nativeEvent.isComposing && !event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey
          && !event.target.closest("input,textarea,select,[contenteditable]")) {
          const key = event.key.toLowerCase();
          const control = key === "m" ? "[data-call-mic]" : key === "v" ? "[data-call-camera]" : undefined;
          const button = control && root.current.querySelector<HTMLButtonElement>(control);
          if (button && !button.disabled) { event.preventDefault(); button.click(); return; }
        }
        if (event.key !== "Escape") return;
        if (fullscreen === "native" || document.fullscreenElement) return;
        event.preventDefault();
        if (fullscreen === "app") setFullscreen(null); else collapse();
      }}>
      <header className="call-stage-header flex min-h-[66px] min-w-0 items-center justify-between gap-2 px-4 py-2 sm:px-6">
        <div className="min-w-0">
          <h2 ref={heading} tabIndex={-1} className="text-sm font-semibold outline-none [overflow-wrap:anywhere] sm:text-base">{name}</h2>
          <div className="flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
            <span role="status">{status}</span>{elapsed ? <span className="tabular-nums" data-call-duration>{elapsed}</span> : null}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <Button variant="ghost" size="icon" className="min-h-11 min-w-11" aria-label="Minimize call" onClick={collapse}><Shrink /></Button>
          <Button variant="ghost" size="icon" className="min-h-11 min-w-11" aria-label={fullscreen ? "Exit full screen" : "Full screen"} onClick={toggleFullscreen}>{fullscreen ? <Minimize2 /> : <Maximize2 />}</Button>
          <div className="call-header-end"><CallEndButton compact /></div>
        </div>
      </header>
      <div className="call-stage-body flex min-h-0 min-w-0 flex-col gap-2 px-3 py-2 sm:px-6">
        <CallStage call={call} media={view.media} userID={manager.userID} profiles={profiles} />
        {audio.needsGesture ? <Button variant="outline" className="min-h-11 self-center" onClick={audio.play}>Play audio</Button> : null}
        {view.error || audio.error ? <p role="alert" className="shrink-0 text-xs text-destructive">{view.error?.message || audio.error}</p> : null}
        {view.canRetry ? <Button variant="outline" className="min-h-11 self-center" onClick={() => manager.retryAction()}>Try again</Button> : null}
      </div>
      {expanded ? <CallControls profiles={profiles} active={active} migrating={migrating} /> : null}
    </section>
  );
}
