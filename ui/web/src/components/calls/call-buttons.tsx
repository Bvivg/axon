"use client";

import { Phone, Video } from "lucide-react";
import { useCall } from "@/components/calls/call-provider";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { CallMode, CallState } from "@/gen/axon/calls/v1/calls_pb";
import type { CallTarget } from "@/lib/calls/manager";

export function CallButtons({ target, name, onStart, labeled = false }: { target?: CallTarget; name?: string; onStart?: () => void; labeled?: boolean }) {
  const { manager, view } = useCall();
  const disabled = !target || !view.available || view.pending || (!!view.call && view.call.state !== CallState.ENDED);
  if (target && "roomId" in target && target.group && !view.sfuAvailable) return null;
  function start(mode: CallMode) {
    if (disabled || !target) return;
    onStart?.();
    void manager.start(target, mode);
  }
  const suffix = name ? ` with ${name}` : "";

  if (!labeled) return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" size="icon" className="size-11 shrink-0" disabled={disabled} aria-label={`Start call${suffix}`}><Phone /></Button>} />
      <DropdownMenuContent align="end" className="w-40">
        <DropdownMenuItem onClick={() => start(CallMode.AUDIO)}><Phone /> Audio call</DropdownMenuItem>
        <DropdownMenuItem onClick={() => start(CallMode.VIDEO)}><Video /> Video call</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  return <>
    <Button variant="signal" className="min-h-11" aria-label={`Audio call${suffix}`} disabled={disabled} onClick={() => start(CallMode.AUDIO)}><Phone /> Audio call</Button>
    <Button variant="outline" className="min-h-11" aria-label={`Video call${suffix}`} disabled={disabled} onClick={() => start(CallMode.VIDEO)}><Video /> Video call</Button>
  </>;
}
