"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { Check, CheckCheck } from "lucide-react";
import { memo, useEffect, useLayoutEffect, useRef } from "react";

import { Avatar } from "@/components/ui/avatar";
import { cn } from "@/lib/utils";
import type { WireMessage } from "@/lib/ws/protocol";

interface TranscriptProps {
  messages: WireMessage[];

  selfID: string;

  names: Map<string, string>;

  avatars?: Map<string, string>;

  readSeqs?: Map<string, number>;

  onVisibleSeqChange?: (seq: number) => void;

  onLoadOlder?: () => void;

  hasOlder?: boolean;

  loadingOlder?: boolean;
}

const loadOlderThreshold = 200;
const atBottomThreshold = 80;

export const Transcript = memo(function Transcript({
  messages,
  selfID,
  names,
  avatars,
  readSeqs,
  onVisibleSeqChange,
  onLoadOlder,
  hasOlder,
  loadingOlder,
}: TranscriptProps) {
  const viewport = useRef<HTMLDivElement>(null);
  const previousScrollHeight = useRef(0);
  const fetchingOlder = useRef(false);
  const atBottom = useRef(true);

  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => viewport.current,

    estimateSize: () => 64,
    overscan: 8,
    useFlushSync: false,
  });

  const virtualizerRef = useRef(virtualizer);
  useEffect(() => {
    virtualizerRef.current = virtualizer;
  });

  const onVisibleSeqChangeRef = useRef(onVisibleSeqChange);
  useEffect(() => {
    onVisibleSeqChangeRef.current = onVisibleSeqChange;
  });

  const virtualItems = virtualizer.getVirtualItems();
  const lastVisibleIndex = virtualItems.length > 0 ? virtualItems[virtualItems.length - 1].index : -1;

  useEffect(() => {
    if (lastVisibleIndex < 0) {
      return;
    }
    const message = messages[lastVisibleIndex];
    if (message) {
      onVisibleSeqChangeRef.current?.(message.seq);
    }
  }, [lastVisibleIndex, messages]);

  const lastSeq = messages.at(-1)?.seq ?? 0;
  useLayoutEffect(() => {
    if (previousScrollHeight.current) {
      const el = viewport.current;
      if (el) {
        el.scrollTop += el.scrollHeight - previousScrollHeight.current;
      }
      previousScrollHeight.current = 0;
      return;
    }

    if (messages.length > 0 && atBottom.current) {
      virtualizerRef.current.scrollToIndex(messages.length - 1, { align: "end" });
    }
  }, [lastSeq, messages.length]);

  useEffect(() => {
    if (!loadingOlder) {
      fetchingOlder.current = false;
    }
  }, [loadingOlder]);

  const onScroll = () => {
    const el = viewport.current;
    if (!el) {
      return;
    }

    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < atBottomThreshold;

    if (!onLoadOlder || fetchingOlder.current || loadingOlder || !hasOlder) {
      return;
    }
    if (el.scrollTop < loadOlderThreshold) {
      fetchingOlder.current = true;
      previousScrollHeight.current = el.scrollHeight;
      onLoadOlder();
    }
  };

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
    <div ref={viewport} onScroll={onScroll} className="min-h-0 flex-1 overflow-y-auto px-1">
      <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
        {virtualItems.map((row) => {
          const message = messages[row.index];
          const mine = message.author_id === selfID;
          const avatarSrc = avatars?.get(message.author_id);
          const name = names.get(message.author_id) ?? shortID(message.author_id);
          const read =
            mine && readSeqs
              ? [...readSeqs.values()].some((seq) => seq >= message.seq)
              : false;

          return (
            <div
              key={message.id}
              ref={virtualizer.measureElement}
              data-index={row.index}
              className="absolute left-0 top-0 w-full"
              style={{ transform: `translateY(${row.start}px)` }}
            >
              <div
                className={cn(
                  "flex items-end gap-2 px-2 py-1",
                  mine ? "justify-end" : "justify-start",
                )}
              >
                {mine ? null : (
                  <Avatar
                    src={avatarSrc}
                    alt=""
                    fallback={name.slice(0, 1).toUpperCase()}
                    className="size-7 shrink-0"
                    sizes="28px"
                  />
                )}

                <div
                  className={cn(
                    "max-w-[75%] rounded-2xl px-3 py-2",
                    mine
                      ? "rounded-br-sm bg-primary text-primary-foreground"
                      : "rounded-bl-sm bg-muted text-foreground",
                  )}
                >
                  {mine ? null : (
                    <p className="text-xs font-medium text-muted-foreground">{name}</p>
                  )}
                  <p className="whitespace-pre-wrap break-words text-sm">{message.body}</p>
                  <div
                    className={cn(
                      "mt-0.5 flex items-center justify-end gap-1",
                      mine ? "text-primary-foreground/70" : "text-muted-foreground",
                    )}
                  >
                    <time className="text-[10px]" dateTime={message.sent_at}>
                      {formatTime(message.sent_at)}
                    </time>
                    {mine ? (
                      read ? (
                        <CheckCheck className="size-3.5 shrink-0" />
                      ) : (
                        <Check className="size-3.5 shrink-0" />
                      )
                    ) : null}
                  </div>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
});

function shortID(id: string): string {
  return id.slice(0, 8);
}

function formatTime(iso: string): string {
  if (!iso) return "";

  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return "";

  return at.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}
