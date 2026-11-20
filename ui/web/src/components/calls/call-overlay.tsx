"use client";

import { useCall } from "@/components/calls/call-provider";
import { CallShell } from "@/components/calls/call-shell";
import { CallPrompt } from "@/components/calls/call-prompt";
import { CallState } from "@/gen/axon/calls/v1/calls_pb";
import { PhoneOff } from "lucide-react";
import Link from "next/link";
import { useEffect, useRef } from "react";
import { useCallDisplay } from "@/components/calls/call-display";
import { Button, buttonVariants } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";

export function CallOverlay() {
  const { view, incoming, elsewhere, expandedResult } = useCall();
  return <>
    {view.call && view.call.state !== CallState.ENDED && !incoming && !elsewhere ? <CallShell key={view.call.id} call={view.call} /> : null}
    {expandedResult ? <CallResult /> : null}
    <CallPrompt />
  </>;
}

function CallResult() {
  const { manager } = useCall();
  const { name, status, duration } = useCallDisplay();
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, []);
  return <ScrollArea data-call-result className="absolute inset-0 z-30 bg-background" role="region" aria-label="Call result">
    <div className="grid min-h-full place-items-center p-4">
      <div className="w-full max-w-md space-y-5 rounded-2xl border border-border bg-card p-6 text-center shadow-overlay motion-safe:animate-fade">
        <span className="mx-auto grid size-14 place-items-center rounded-2xl bg-muted text-muted-foreground"><PhoneOff aria-hidden /></span>
        <div role="status" aria-atomic="true">
          <h2 ref={heading} tabIndex={-1} className="text-2xl font-semibold tracking-tight outline-none">{status}</h2>
          <p className="mt-1 text-sm text-muted-foreground [overflow-wrap:anywhere]">{name}{duration === undefined ? null : ` · ${Math.floor(duration / 60)}:${String(duration % 60).padStart(2, "0")}`}</p>
        </div>
        <div className="flex flex-wrap justify-center gap-2">
          <Button className="min-h-11" onClick={() => manager.dismiss()}>Done</Button>
          <Link className={buttonVariants({ variant: "outline", className: "min-h-11" })} href="/calls" onClick={() => manager.dismiss()}>Call history</Link>
        </div>
      </div>
    </div>
  </ScrollArea>;
}
