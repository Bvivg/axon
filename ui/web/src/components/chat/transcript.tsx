"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useLayoutEffect, useRef } from "react";

import type { WireMessage } from "@/lib/ws/protocol";

interface TranscriptProps {
  messages: WireMessage[];

  /** The reader, so their own messages can be told apart. */
  selfID: string;

  /** User id to display name, from the room's member list. */
  names: Map<string, string>;
}

/**
 * The conversation.
 *
 * Virtualised, as rules/nextjs-web.md asks for long lists: a room that has been
 * going for a while holds thousands of messages, and rendering them all costs
 * the same whether or not anybody scrolls back. Heights vary with the length of
 * what people wrote, so each row is measured rather than assumed.
 */
export function Transcript({ messages, selfID, names }: TranscriptProps) {
  const viewport = useRef<HTMLDivElement>(null);

  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => viewport.current,
    // A starting guess only — measureElement below replaces it with the real
    // height once a row has rendered.
    estimateSize: () => 64,
    overscan: 8,
  });

  // Follow the conversation. useLayoutEffect rather than useEffect so the jump
  // happens in the same frame the message is painted; with useEffect the reader
  // sees the old position for a frame and the view visibly lurches.
  const lastSeq = messages.at(-1)?.seq ?? 0;
  useLayoutEffect(() => {
    if (messages.length > 0) {
      virtualizer.scrollToIndex(messages.length - 1, { align: "end" });
    }
  }, [lastSeq, messages.length, virtualizer]);

  // The first paint has no measured rows yet, so the scroll above lands short.
  // One more pass after the sizes are known puts it at the bottom.
  useEffect(() => {
    if (messages.length > 0) {
      virtualizer.scrollToIndex(messages.length - 1, { align: "end" });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
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

/**
 * shortID names somebody the member list does not cover.
 *
 * It happens: the name is a snapshot taken when a person joined, and somebody
 * who left the room still has their messages in it. A short id is a poor label
 * but an honest one — better than attributing the message to nobody.
 */
function shortID(id: string): string {
  return id.slice(0, 8);
}

function formatTime(iso: string): string {
  if (!iso) return "";

  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return "";

  return at.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}
