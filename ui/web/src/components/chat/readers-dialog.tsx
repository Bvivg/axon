"use client";

import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { NameOf } from "@/lib/chat/content";
import { describe } from "@/lib/errors";
import { clockTime, dayLabel } from "@/lib/format";
import { useMessageReaders } from "@/lib/query/chat";

const when = (at: Date) => `${dayLabel(at)}, ${clockTime(at)}`;

export function ReadersDialog({
  messageID,
  direct,
  nameOf,
  onOpenChange,
}: {
  messageID: string | null;
  direct: boolean;
  nameOf: NameOf;
  onOpenChange: (open: boolean) => void;
}) {
  const readers = useMessageReaders(messageID);
  const list = readers.data ?? [];

  return (
    <Dialog open={messageID !== null} onOpenChange={onOpenChange}>
      <DialogContent className="space-y-3">
        <DialogTitle>{direct ? "Read" : "Read by"}</DialogTitle>
        {readers.isPending ? (
          <p className="text-sm text-muted-foreground">Loading…</p>
        ) : readers.isError ? (
          <p className="text-sm text-destructive">{describe(readers.error)}</p>
        ) : list.length === 0 ? (
          <p className="text-sm text-muted-foreground">Not read yet.</p>
        ) : direct ? (
          <p className="text-sm">{list[0].readAt ? `Read ${when(list[0].readAt)}` : "Read"}</p>
        ) : (
          <ScrollArea className="max-h-72">
            <ul className="divide-y divide-border">
              {list.map((reader) => (
                <li key={reader.userId} className="flex items-center justify-between gap-3 py-2 text-sm">
                  <span className="truncate font-medium">{nameOf(reader.userId)}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {reader.readAt ? when(reader.readAt) : ""}
                  </span>
                </li>
              ))}
            </ul>
          </ScrollArea>
        )}
      </DialogContent>
    </Dialog>
  );
}
