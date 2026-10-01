"use client";

import { useState } from "react";

import { useRoomDirectory } from "@/components/chat/use-room-directory";
import { Avatar } from "@/components/ui/avatar";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";

export function ForwardDialog({
  open,
  onOpenChange,
  onPick,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPick: (roomID: string) => void;
}) {
  const { entries } = useRoomDirectory();
  const [query, setQuery] = useState("");
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) setQuery("");
  }

  const needle = query.trim().toLowerCase();
  const matches = needle ? entries.filter((entry) => entry.label.toLowerCase().includes(needle)) : entries;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="space-y-3">
        <DialogTitle>Forward to…</DialogTitle>
        <Input
          aria-label="Find a chat"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Find a chat"
        />
        <ScrollArea className="max-h-72">
          <ul className="divide-y divide-border">
            {matches.map((entry) => (
              <li key={entry.room.id}>
                <button
                  type="button"
                  onClick={() => onPick(entry.room.id)}
                  className="flex w-full items-center gap-3 rounded-md px-2 py-2 text-left text-sm transition-colors hover:bg-muted"
                >
                  <Avatar
                    src={entry.avatarSrc}
                    alt=""
                    fallback={(entry.label || "?").slice(0, 1).toUpperCase()}
                    className="size-9"
                    sizes="36px"
                  />
                  <span className="truncate font-medium">{entry.label}</span>
                </button>
              </li>
            ))}
            {matches.length === 0 ? (
              <li className="px-2 py-6 text-center text-sm text-muted-foreground">No chat matches.</li>
            ) : null}
          </ul>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  );
}
