"use client";

import { Mic, MicOff, Phone, PhoneOff, X } from "lucide-react";
import Link from "next/link";

import { useCallAudio } from "@/components/calls/call-audio-renderer";
import { CallEndButton } from "@/components/calls/call-controls";
import { useCallDisplay } from "@/components/calls/call-display";
import { useCall } from "@/components/calls/call-provider";
import { Button, buttonVariants } from "@/components/ui/button";
import { CallState } from "@/gen/axon/calls/v1/calls_pb";
import { cn } from "@/lib/utils";

export function CallMiniBar({ placement }: { placement: "app" | "chat" }) {
  const { expanded, expandedResult, incoming, view } = useCall();
  if (expanded || expandedResult || incoming || !view.call) return null;
  return <CallStrip placement={placement} />;
}

function CallStrip({ placement }: { placement: "app" | "chat" }) {
  const { manager, view, elsewhere, expand } = useCall();
  const { name, status, elapsed } = useCallDisplay();
  const audio = useCallAudio();
  const media = view.media;
  const ended = !view.call || view.call.state === CallState.ENDED;
  const Icon = ended ? PhoneOff : Phone;
  return (
    <aside aria-label="Ongoing call"
      data-call-mini={placement}
      className={cn("flex min-w-0 shrink-0 flex-wrap items-center justify-between gap-x-2 gap-y-1 border-t border-border bg-card px-2 py-1 sm:px-3", ended && "border-signal/40 bg-signal/8 py-2", placement === "app" ? "call-app-strip" : "call-chat-strip")}>
      <div className="flex min-w-0 grow basis-24 items-center gap-2">
        <Icon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0">
          <p className="text-sm font-medium [overflow-wrap:anywhere]">{name}</p>
          <div className="flex flex-wrap gap-x-2 text-xs text-muted-foreground">
            <span role="status" className={ended ? "text-sm font-semibold text-foreground" : undefined}>{status}</span>{elapsed ? <span className="tabular-nums" data-call-duration>{elapsed}</span> : null}
          </div>
        </div>
      </div>
      <div className="ml-auto flex max-w-full flex-wrap items-center justify-end gap-1">
        {media && !ended && !elsewhere ? <Button variant="ghost" size="icon" className="min-h-11 min-w-11" aria-label={media.muted ? "Unmute microphone" : "Mute microphone"} aria-pressed={media.muted} onClick={() => media.mute()}>{media.muted ? <MicOff /> : <Mic />}</Button> : null}
        {audio.needsGesture ? <Button variant="outline" size="sm" className="min-h-11" onClick={audio.play}>Play audio</Button> : null}
        <CallEndButton compact />
        {ended ? <Link href="/calls" onClick={() => manager.dismiss()} className={buttonVariants({ variant: "outline", size: "sm", className: "min-h-11" })}>Call history</Link> : null}
        {ended || elsewhere ? <Button variant="ghost" size="icon" className="min-h-11 min-w-11" aria-label="Dismiss call" onClick={() => manager.dismiss()}><X /></Button>
          : <Button data-call-return variant="outline" size="sm" className="min-h-11" onClick={expand}>Return to call</Button>}
      </div>
      {view.error || audio.error ? <p role="alert" className="basis-full text-xs text-destructive">{view.error?.message || audio.error}</p> : null}
      {view.canRetry ? <Button variant="outline" className="min-h-11" onClick={() => manager.retryAction()}>Try again</Button> : null}
    </aside>
  );
}
