"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useLayoutEffect, useRef } from "react";

import type { WireMessage } from "@/lib/ws/protocol";

interface TranscriptProps {
  messages: WireMessage[];

  selfID: string;

  names: Map<string, string>;
}

export function Transcript({ messages, selfID, names }: TranscriptProps) {
  const viewport = useRef<HTMLDivElement>(null);

  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => viewport.current,

    estimateSize: () => 64,
    overscan: 8,
  });

  const lastSeq = messages.at(-1)?.seq ?? 0;
  useLayoutEffect(() => {
    if (messages.length > 0) {
      virtualizer.scrollToIndex(messages.length - 1, { align: "end" });
    }
  }, [lastSeq, messages.length, virtualizer]);

  useEffect(() => {
    if (messages.length > 0) {
      virtualizer.scrollToIndex(messages.length - 1, { align: "end" });
    }

  }, []);

  if (messages.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-muted-foreground">
          Nothing said yet. Go ahead.
        </p>
      </div>
    );
  }

  return (
    <div ref={viewport} className="flex-1 overflow-y-auto px-1">
      <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((row) => {
          const message = messages[row.index];
          const mine = message.author_id === selfID;

          return (
            <div
              key={message.id}
              ref={virtualizer.measureElement}
              data-index={row.index}
              className="absolute left-0 top-0 w-full"
              style={{ transform: `translateY(${row.start}px)` }}
            >
              <article className="px-2 py-1.5">
                <header className="flex items-baseline gap-2">
                  <span
                    className={
                      mine
                        ? "text-sm font-medium text-primary"
                        : "text-sm font-medium"
                    }
                  >
                    {mine ? "You" : names.get(message.author_id) ?? shortID(message.author_id)}
                  </span>
                  <time className="text-xs text-muted-foreground" dateTime={message.sent_at}>
                    {formatTime(message.sent_at)}
                  </time>
                </header>
                <p className="whitespace-pre-wrap break-words text-sm">
                  {message.body}
                </p>
              </article>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function shortID(id: string): string {
  return id.slice(0, 8);
}

function formatTime(iso: string): string {
  if (!iso) return "";

  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return "";

  return at.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}
