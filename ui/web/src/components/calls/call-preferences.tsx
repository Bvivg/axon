"use client";

import { Settings2 } from "lucide-react";
import Link from "next/link";
import { useCall } from "@/components/calls/call-provider";
import { useSounds } from "@/components/nav/sound-provider";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogTitle, DialogTrigger } from "@/components/ui/dialog";

export function CallPreferences() {
  const { shortcuts, setShortcuts } = useCall();
  const { calls, messages, setCalls, setMessages } = useSounds();
  return (
    <Dialog>
      <DialogTrigger render={<Button variant="ghost" size="icon" aria-label="Call settings"><Settings2 /></Button>} />
      <DialogContent className="flex flex-col gap-5">
        <DialogTitle>Call settings</DialogTitle>
        <div className="grid divide-y divide-border rounded-lg border border-border bg-background/50">
          {[
            { label: "Call sounds", checked: calls, change: setCalls },
            { label: "Message sounds", checked: messages, change: setMessages },
            { label: "M and V shortcuts in calls", checked: shortcuts, change: setShortcuts },
          ].map(({ label, checked, change }) => (
            <label key={label} className="flex min-h-11 cursor-pointer items-center gap-3 px-3 py-2.5 text-sm">
              <input
                type="checkbox"
                className="size-4 shrink-0 accent-signal focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-signal"
                checked={checked}
                onChange={(event) => change(event.target.checked)}
              />
              <span>{label}</span>
            </label>
          ))}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t border-border pt-4">
          <DialogClose render={<Link href="/profile/notifications" className="inline-flex min-h-11 items-center text-sm font-medium text-signal hover:underline" />}>Notification settings</DialogClose>
          <DialogClose render={<Button variant="signal" className="ml-auto min-h-11" />}>Done</DialogClose>
        </div>
      </DialogContent>
    </Dialog>
  );
}
