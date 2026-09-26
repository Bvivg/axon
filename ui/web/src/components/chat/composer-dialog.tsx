"use client";

import { closeComposer, useComposerOpen } from "@/components/chat/composer-store";
import { NewChatDialog } from "@/components/chat/new-chat-dialog";
import { Dialog, DialogContent } from "@/components/ui/dialog";

export function ComposerDialog() {
  const open = useComposerOpen();

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : closeComposer())}>
      <DialogContent className="w-[min(90vw,28rem)] rounded-xl border border-border bg-card p-4 shadow-2xl">
        {open ? <NewChatDialog onDone={closeComposer} /> : null}
      </DialogContent>
    </Dialog>
  );
}
