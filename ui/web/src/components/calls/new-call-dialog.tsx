"use client";

import { Phone } from "lucide-react";
import { useState } from "react";

import { CallButtons } from "@/components/calls/call-buttons";
import { UserSearch } from "@/components/chat/people-picker";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { useSession } from "@/lib/auth/session";
import { profileName } from "@/lib/format";

export function NewCallDialog({ disabled = false, compact = false, label = "New call" }: { disabled?: boolean; compact?: boolean; label?: string }) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {compact ? (
        <DialogTrigger render={<Button variant="signal" size="icon" className="size-9 text-signal-foreground hover:text-signal-foreground" aria-label="New call" disabled={disabled} />}>
          <Phone />
        </DialogTrigger>
      ) : (
        <DialogTrigger render={<Button variant="signal" className="min-h-10" disabled={disabled} />}>
          <Phone />{label}
        </DialogTrigger>
      )}
      {open ? <NewCallPicker onStart={() => setOpen(false)} /> : null}
    </Dialog>
  );
}

function NewCallPicker({ onStart }: { onStart: () => void }) {
  const { user } = useSession();
  const [peer, setPeer] = useState<PublicProfile>();
  return (
    <DialogContent className="flex max-h-[calc(100dvh-2rem)] flex-col gap-4 overflow-y-auto">
      <DialogTitle>New call</DialogTitle>
      <UserSearch
        id="new-call-search"
        label="Search people"
        hidden={person => person.id === user?.id}
        pressed={person => person.id === peer?.id}
        trailing={() => null}
        onPick={setPeer}
        className="max-h-56"
      />
      {peer ? <p className="break-words text-sm font-medium">{profileName(peer)}</p> : null}
      <div className="flex flex-wrap gap-2">
        <CallButtons target={peer ? { peerUserId: peer.id } : undefined} onStart={onStart} labeled />
        <DialogClose render={<Button variant="ghost" className="min-h-11" />}>Cancel</DialogClose>
      </div>
    </DialogContent>
  );
}
