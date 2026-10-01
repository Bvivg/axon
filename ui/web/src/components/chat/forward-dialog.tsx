"use client";

import { CheckCircle2, Circle } from "lucide-react";
import { useState } from "react";

import { useRoomDirectory } from "@/components/chat/use-room-directory";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";

export const maxForwardTargets = 50;

export function ForwardDialog({
  open,
  count,
  pending,
  error,
  onOpenChange,
  onForward,
}: {
  open: boolean;
  count: number;
  pending: boolean;
  error: string | null;
  onOpenChange: (open: boolean) => void;
  onForward: (roomIDs: string[], comment: string) => void;
}) {
  const { entries } = useRoomDirectory();
  const [query, setQuery] = useState("");
  const [comment, setComment] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setQuery("");
      setComment("");
      setPicked([]);
    }
  }

  const needle = query.trim().toLowerCase();
  const matches = needle ? entries.filter((entry) => entry.label.toLowerCase().includes(needle)) : entries;
  const full = picked.length >= maxForwardTargets;

  const toggle = (roomID: string) =>
    setPicked((current) =>
      current.includes(roomID) ? current.filter((id) => id !== roomID) : full ? current : [...current, roomID],
    );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="space-y-3">
        <DialogTitle>{count === 1 ? "Forward message to…" : `Forward ${count} messages to…`}</DialogTitle>
        <Input
          aria-label="Find a chat"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Find a chat"
        />
        <ScrollArea className="max-h-72">
          <ul className="divide-y divide-border">
            {matches.map((entry) => {
              const checked = picked.includes(entry.room.id);
              return (
                <li key={entry.room.id}>
                  <button
                    type="button"
                    role="checkbox"
                    aria-checked={checked}
                    disabled={!checked && full}
                    onClick={() => toggle(entry.room.id)}
                    className={cn(
                      "flex w-full items-center gap-3 rounded-md px-2 py-2 text-left text-sm transition-colors hover:bg-muted disabled:opacity-50",
                      checked && "bg-signal/10",
                    )}
                  >
                    <Avatar
                      src={entry.avatarSrc}
                      alt=""
                      fallback={(entry.label || "?").slice(0, 1).toUpperCase()}
                      className="size-9"
                      sizes="36px"
                    />
                    <span className="min-w-0 flex-1 truncate font-medium">{entry.label}</span>
                    {checked ? (
                      <CheckCircle2 aria-hidden className="size-5 shrink-0 fill-signal text-signal-foreground" />
                    ) : (
                      <Circle aria-hidden className="size-5 shrink-0 text-muted-foreground" />
                    )}
                  </button>
                </li>
              );
            })}
            {matches.length === 0 ? (
              <li className="px-2 py-6 text-center text-sm text-muted-foreground">No chat matches.</li>
            ) : null}
          </ul>
        </ScrollArea>
        <Input
          aria-label="Comment"
          value={comment}
          onChange={(event) => setComment(event.target.value)}
          placeholder="Add a comment"
          maxLength={4096}
        />
        {error ? <p className="text-sm text-destructive">{error}</p> : null}
        <div className="flex items-center justify-end gap-2">
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant="signal"
            disabled={picked.length === 0 || pending}
            aria-busy={pending}
            onClick={() => onForward(picked, comment.trim())}
          >
            {picked.length > 1 ? `Forward to ${picked.length} chats` : "Forward"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
