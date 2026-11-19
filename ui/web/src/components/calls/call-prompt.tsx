"use client";

import { Mic, Phone, PhoneOff, Video } from "lucide-react";
import { useRef } from "react";
import { useCall } from "@/components/calls/call-provider";
import { useCallDisplay } from "@/components/calls/call-display";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { CallMode } from "@/gen/axon/calls/v1/calls_pb";
import { initials } from "@/lib/format";
import { useRoom } from "@/lib/query/chat";

export function CallPrompt() {
  const { view, incoming } = useCall();
  if (!incoming && view.waitingForDevice === undefined && (!view.starting || view.call) && (view.call || !view.error)) return null;
  return <Prompt />;
}

function Prompt() {
  const { manager, view, incoming, expand } = useCall();
  const { name, profiles } = useCallDisplay();
  const title = useRef<HTMLHeadingElement>(null);
  const call = view.call;
  const group = call?.kind === "group";
  const room = useRoom(incoming && group ? call.roomId ?? "" : "");
  const caller = call?.callerId ? profiles?.get(call.callerId) : undefined;
  const waiting = view.waitingForDevice !== undefined;
  const starting = !call && view.starting;
  const video = call?.mode === CallMode.VIDEO;
  const heading = incoming ? "Incoming call"
    : waiting ? "Waiting for access"
    : starting ? (starting === "cancelling" ? "Cancelling call…" : "Starting call…")
    : "Could not start the call";
  const who = group ? room.data?.room?.name || name : name;
  const cancel = () => { if (waiting || starting) manager.cancelCapture(); else if (!incoming) manager.dismiss(); };

  return (
    <Dialog open disablePointerDismissal onOpenChange={open => { if (!open) cancel(); }}>
      <DialogContent initialFocus={title} data-incoming-call={incoming || undefined} className={`max-h-[calc(100dvh-2rem)] space-y-4 overflow-y-auto ${incoming ? "border-signal/35 p-5 sm:p-7" : ""}`}>
        <DialogTitle ref={title} tabIndex={-1} className={incoming ? "text-center text-lg" : undefined}>{heading}</DialogTitle>
        {incoming ? <div className="flex min-w-0 flex-col items-center gap-4 rounded-xl bg-signal/8 p-4 text-center">
          <Avatar src={group ? undefined : caller?.avatarUrls?.small || caller?.avatarUrl} alt="" fallback={initials(who)} className="size-18 shrink-0 rounded-2xl text-xl" />
          <div className="min-w-0">
            <p className="text-xl font-semibold tracking-tight [overflow-wrap:anywhere]">{who}</p>
            <p className="flex items-center justify-center gap-2 text-sm text-muted-foreground">{video ? <Video aria-hidden size={16} /> : <Phone aria-hidden size={16} />}{video ? "Video call" : "Audio call"}</p>
          </div>
        </div> : null}
        {waiting ? <p role="status" className="text-sm">Allow {view.waitingForDevice === CallMode.VIDEO ? "microphone and camera" : "microphone"} access in your browser.</p> : null}
        {view.error ? <p role="alert" className="text-sm text-destructive">{view.error.message}</p> : null}
        {waiting || !incoming ? <div className="flex flex-wrap gap-2">
          {starting !== "cancelling" ? <Button variant="outline" className="min-h-11" onClick={cancel}>Cancel</Button> : null}
          {view.canRetry ? <Button className="min-h-11" onClick={() => manager.retryAction()}>Try again</Button> : null}
        </div> : <div className="grid grid-cols-2 gap-2">
          <Button variant="destructive" className="min-h-12" disabled={view.ending || view.pending} onClick={() => { void manager.end(); }}><PhoneOff />Decline</Button>
          <Button variant="signal" className="min-h-12" disabled={view.ending || view.pending} onClick={() => { expand(); void manager.accept(); }}><Phone />Accept</Button>
          {video ? <Button variant="outline" className="col-span-2 min-h-11" disabled={view.ending || view.pending} onClick={() => { expand(); void manager.accept(true); }}><Mic />Answer with audio</Button> : null}
          {view.canRetry ? <Button variant="outline" className="col-span-2 min-h-11" onClick={() => manager.retryAction()}>Try again</Button> : null}
        </div>}
      </DialogContent>
    </Dialog>
  );
}
