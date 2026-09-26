"use client";

import { SquarePen } from "lucide-react";
import { useState } from "react";

import { NewChatDialog } from "@/components/chat/new-chat-dialog";
import { Dialog, DialogContent, DialogTrigger } from "@/components/ui/dialog";

export function ChatListHeader() {
  const [open, setOpen] = useState(false);

  return (
    <header className="flex shrink-0 items-center justify-between border-b border-border px-4 py-4">
      <div className="size-9" />
      <h1 className="text-lg font-semibold">Chats</h1>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogTrigger
          aria-label="New chat"
          className="flex size-9 items-center justify-center rounded-full text-muted-foreground outline-none transition-colors hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
        >
          <SquarePen className="size-5" />
        </DialogTrigger>
        <DialogContent className="w-[min(90vw,28rem)] rounded-xl border border-border bg-card p-4 shadow-2xl">
          <NewChatDialog onDone={() => setOpen(false)} />
        </DialogContent>
      </Dialog>
    </header>
  );
}
