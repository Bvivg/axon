"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { Ban, Check, CheckCheck, Copy, Forward, MoreHorizontal, Pencil, Reply, Trash2 } from "lucide-react";
import { memo, useEffect, useLayoutEffect, useRef, useState } from "react";

import { MessageMedia } from "@/components/chat/message-content";
import { Avatar } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ContentKind, contentOf, isDeleted, isEditable, quoteOf, systemTextOf, type NameOf } from "@/lib/chat/content";
import { dayLabel, fromISO, isSameDay } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { WireMessage } from "@/lib/ws/protocol";

export interface MessageActions {
  onReply: (message: WireMessage) => void;
  onForward: (message: WireMessage) => void;
  onEdit: (message: WireMessage) => void;
  onDelete: (message: WireMessage) => void;
}

interface TranscriptProps {
  messages: WireMessage[];

  selfID: string;

  nameOf: NameOf;

  avatars?: Map<string, string>;

  readSeqs?: Map<string, number>;

  onVisibleSeqChange?: (seq: number) => void;

  onLoadOlder?: () => void;

  hasOlder?: boolean;

  loadingOlder?: boolean;

  onOpenMedia?: (message: WireMessage) => void;

  actions?: MessageActions;

  showNames?: boolean;
}

const loadOlderThreshold = 200;
const atBottomThreshold = 80;
const flashMs = 1600;

export const Transcript = memo(function Transcript({
  messages,
  selfID,
  nameOf,
  avatars,
  readSeqs,
  onVisibleSeqChange,
  onLoadOlder,
  hasOlder,
  loadingOlder,
  onOpenMedia,
  actions,
  showNames = true,
}: TranscriptProps) {
  const viewport = useRef<HTMLDivElement>(null);
  const previousScrollHeight = useRef(0);
  const fetchingOlder = useRef(false);
  const atBottom = useRef(true);
  const [menuFor, setMenuFor] = useState<string | null>(null);
  const [flashID, setFlashID] = useState<string | null>(null);

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
      virtualizerRef.current.scrollToIndex(messages.length - 1, {
        align: "end",
      });
    }
  }, [lastSeq, messages.length]);

  useEffect(() => {
    if (!loadingOlder) {
      fetchingOlder.current = false;
    }
  }, [loadingOlder]);

  useEffect(() => {
    if (!flashID) {
      return;
    }
    const timer = setTimeout(() => setFlashID(null), flashMs);
    return () => clearTimeout(timer);
  }, [flashID]);

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

  const jumpTo = (messageID: string) => {
    const index = messages.findIndex((m) => m.id === messageID);
    if (index < 0) {
      return;
    }
    virtualizer.scrollToIndex(index, { align: "center" });
    setFlashID(messageID);
  };

  if (messages.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-muted-foreground">Nothing said yet. Go ahead.</p>
      </div>
    );
  }

  return (
    <ScrollArea className="flex-1" viewportProps={{ ref: viewport, onScroll, className: "px-1" }}>
      <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
        {virtualItems.map((row) => {
          const message = messages[row.index];
          const day = dayStartedBy(message, messages[row.index - 1]);

          return (
            <div
              key={message.id}
              ref={virtualizer.measureElement}
              data-index={row.index}
              className="absolute left-0 top-0 w-full"
              style={{ transform: `translateY(${row.start}px)` }}
            >
              {day ? <DayDivider label={day} /> : null}
              {message.kind === ContentKind.System ? (
                <SystemLine text={systemTextOf(message, nameOf, selfID)} />
              ) : (
                <MessageRow
                  message={message}
                  selfID={selfID}
                  nameOf={nameOf}
                  avatarSrc={avatars?.get(message.author_id)}
                  read={
                    message.author_id === selfID && readSeqs
                      ? [...readSeqs.values()].some((seq) => seq >= message.seq)
                      : false
                  }
                  flash={flashID === message.id}
                  menuOpen={menuFor === message.id}
                  onMenuOpenChange={(open) => setMenuFor(open ? message.id : null)}
                  onOpenMedia={onOpenMedia}
                  onJumpTo={jumpTo}
                  actions={actions}
                  showName={showNames}
                />
              )}
            </div>
          );
        })}
      </div>
    </ScrollArea>
  );
});

function dayStartedBy(message: WireMessage, previous?: WireMessage): string | null {
  const at = fromISO(message.sent_at);
  if (!at) return null;
  const before = previous ? fromISO(previous.sent_at) : null;
  return before && isSameDay(before, at) ? null : dayLabel(at);
}

function DayDivider({ label }: { label: string }) {
  return (
    <div className="flex justify-center px-4 pt-3 pb-1">
      <span className="rounded-full border border-border bg-background px-3 py-0.5 text-xs font-medium text-muted-foreground">
        {label}
      </span>
    </div>
  );
}

function SystemLine({ text }: { text: string }) {
  return (
    <div className="flex justify-center px-4 py-1.5">
      <p className="max-w-[85%] rounded-full bg-muted px-3 py-1 text-center text-xs text-muted-foreground">{text}</p>
    </div>
  );
}

function MessageRow({
  message,
  selfID,
  nameOf,
  avatarSrc,
  read,
  flash,
  menuOpen,
  onMenuOpenChange,
  onOpenMedia,
  onJumpTo,
  actions,
  showName,
}: {
  message: WireMessage;
  selfID: string;
  nameOf: NameOf;
  showName: boolean;
  avatarSrc?: string;
  read: boolean;
  flash: boolean;
  menuOpen: boolean;
  onMenuOpenChange: (open: boolean) => void;
  onOpenMedia?: (message: WireMessage) => void;
  onJumpTo: (messageID: string) => void;
  actions?: MessageActions;
}) {
  const mine = message.author_id === selfID;
  const name = nameOf(message.author_id);
  const deleted = isDeleted(message);
  const content = contentOf(message);
  const isMedia = !deleted && content.kind !== ContentKind.Text;
  const visual = isMedia && (content.kind === ContentKind.Image || content.kind === ContentKind.Video);
  const caption = message.body.trim();
  const padded = cn(visual && "px-2");
  const hasText = deleted || !isMedia || caption !== "";

  const meta = (
    <span
      className={cn(
        "ml-auto flex shrink-0 items-center gap-1",
        mine ? "text-signal-foreground/70" : "text-muted-foreground",
      )}
    >
      {message.edited_at && !deleted ? <span className="text-[10px]">edited</span> : null}
      <time className="text-[10px]" dateTime={message.sent_at}>
        {formatTime(message.sent_at)}
      </time>
      {mine ? read ? <CheckCheck className="size-3.5 shrink-0" /> : <Check className="size-3.5 shrink-0" /> : null}
    </span>
  );

  return (
    <div
      className={cn("group flex items-end gap-2 px-2 py-1", mine ? "flex-row-reverse" : "flex-row")}
      onContextMenu={(event) => {
        if (!actions || deleted) return;
        event.preventDefault();
        onMenuOpenChange(true);
      }}
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

      <article
        aria-label={mine ? "Your message" : `Message from ${name}`}
        className={cn(
          "max-w-[75%] min-w-0 rounded-2xl transition-shadow duration-300",
          visual ? "p-1" : "px-3 py-2",
          mine ? "rounded-br-sm bg-signal text-signal-foreground" : "rounded-bl-sm bg-muted text-foreground",
          flash && "ring-2 ring-signal/60 ring-offset-2 ring-offset-background",
        )}
      >
        {mine || !showName ? null : (
          <p className={cn("text-xs font-medium text-muted-foreground", visual && "px-2 pt-1 pb-1")}>{name}</p>
        )}

        {deleted ? null : (
          <>
            {message.forwarded_from_id ? (
              <p className={cn("text-xs", padded, mine ? "text-signal-foreground/80" : "text-signal")}>
                Forwarded from <span className="font-medium">{nameOf(message.forwarded_from_author_id)}</span>
              </p>
            ) : null}

            {message.reply_to ? (
              <button
                type="button"
                onClick={() => message.reply_to && onJumpTo(message.reply_to.id)}
                aria-label={`Replying to ${nameOf(message.reply_to.author_id)}`}
                className={cn(
                  "my-1 block w-full min-w-0 rounded-md border-l-2 px-2 py-1 text-left",
                  visual && "mx-1 w-[calc(100%-0.5rem)]",
                  mine ? "border-signal-foreground/70 bg-signal-foreground/15" : "border-signal bg-background/60",
                )}
              >
                <span className="block truncate text-xs font-medium">{nameOf(message.reply_to.author_id)}</span>
                <span
                  className={cn(
                    "block truncate text-xs",
                    mine ? "text-signal-foreground/80" : "text-muted-foreground",
                    message.reply_to.deleted && "italic",
                  )}
                >
                  {quoteOf(message.reply_to)}
                </span>
              </button>
            ) : null}

            {isMedia ? (
              <MessageMedia content={content} caption={caption} mine={mine} onOpen={() => onOpenMedia?.(message)} />
            ) : null}
          </>
        )}

        {hasText ? (
          <div
            className={cn(
              "flex flex-wrap items-end gap-x-2 gap-y-0.5",
              visual && "px-2 pt-1 pb-1",
              isMedia && !visual && "pt-1",
            )}
          >
            {deleted ? (
              <p
                className={cn(
                  "flex min-w-0 items-center gap-1.5 text-sm italic",
                  mine ? "text-signal-foreground/80" : "text-muted-foreground",
                )}
              >
                <Ban aria-hidden className="size-3.5 shrink-0" />
                Message deleted
              </p>
            ) : (
              <p
                className={cn(
                  "min-w-0 whitespace-pre-wrap break-words text-sm",
                  message.body.trim().includes("\n") && "basis-full",
                )}
              >
                {message.body}
              </p>
            )}
            {meta}
          </div>
        ) : (
          <div className={cn("mt-0.5 flex", visual && "px-2 pb-1")}>{meta}</div>
        )}
      </article>

      {actions && !deleted ? (
        <MessageMenu message={message} mine={mine} open={menuOpen} onOpenChange={onMenuOpenChange} actions={actions} />
      ) : null}
    </div>
  );
}

function MessageMenu({
  message,
  mine,
  open,
  onOpenChange,
  actions,
}: {
  message: WireMessage;
  mine: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  actions: MessageActions;
}) {
  const text = message.body.trim();

  return (
    <DropdownMenu open={open} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label="Message actions"
            className="mb-1 flex size-7 shrink-0 items-center justify-center rounded-full text-muted-foreground opacity-0 outline-none transition-opacity group-hover:opacity-100 hover:bg-muted hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring data-[popup-open]:opacity-100 pointer-coarse:opacity-100"
          >
            <MoreHorizontal aria-hidden className="size-4" />
          </button>
        }
      />
      <DropdownMenuContent side="top" align={mine ? "end" : "start"} className="w-44">
        <DropdownMenuItem onClick={() => actions.onReply(message)}>
          <Reply />
          Reply
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => actions.onForward(message)}>
          <Forward />
          Forward
        </DropdownMenuItem>
        {text ? (
          <DropdownMenuItem onClick={() => void navigator.clipboard?.writeText(text)}>
            <Copy />
            Copy text
          </DropdownMenuItem>
        ) : null}
        {mine ? (
          <>
            <DropdownMenuSeparator />
            {isEditable(message) ? (
              <DropdownMenuItem onClick={() => actions.onEdit(message)}>
                <Pencil />
                Edit
              </DropdownMenuItem>
            ) : null}
            <DropdownMenuItem
              onClick={() => actions.onDelete(message)}
              className="text-destructive [&>svg]:text-destructive"
            >
              <Trash2 />
              Delete
            </DropdownMenuItem>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function formatTime(iso: string): string {
  if (!iso) return "";

  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return "";

  return at.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  });
}
