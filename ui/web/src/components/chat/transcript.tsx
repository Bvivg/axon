"use client";

import {
  ArrowDown,
  Ban,
  Check,
  CheckCheck,
  CheckCircle2,
  Circle,
  CircleAlert,
  Clock,
  Copy,
  Eye,
  Forward,
  ImageIcon,
  ListChecks,
  Loader2,
  MoreHorizontal,
  Pencil,
  Reply,
  Trash2,
} from "lucide-react";
import {
  memo,
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type Ref,
} from "react";
import { Virtualizer, type VirtualizerHandle } from "virtua";

import { mediaBoxSize, MessageMedia } from "@/components/chat/message-content";
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
import { useDragSelect } from "@/lib/chat/drag-select";
import { splitLinks } from "@/lib/chat/links";
import { canSelect, isPicked, type MessageSelection } from "@/lib/chat/selection";
import { groupTranscript, sameItem, type TranscriptItem } from "@/lib/chat/series";
import { useLongPress, useSwipeToReply } from "@/lib/chat/swipe";
import { clockTime, dayLabel, fromISO } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { WireMessage } from "@/lib/ws/protocol";
import type { OutgoingMessage } from "@/lib/ws/use-chat-socket";

export interface MessageActions {
  onReply: (message: WireMessage) => void;
  onForward: (message: WireMessage) => void;
  onEdit: (message: WireMessage) => void;
  onDelete: (message: WireMessage) => void;
  onSelect: (message: WireMessage) => void;
  onReaders: (message: WireMessage) => void;
  onCopyImage: (message: WireMessage) => void;
}

export interface TranscriptHandle {
  reveal: (messageID: string) => boolean;
  scrollToEnd: () => void;
}

interface TranscriptProps {
  ref?: Ref<TranscriptHandle>;
  messages: WireMessage[];
  outgoing?: OutgoingMessage[];
  connected?: boolean;
  onRetrySend?: (id: string) => void;
  onDismissSend?: (id: string) => void;
  selfID: string;
  nameOf: NameOf;
  avatars?: Map<string, string>;
  readSeqs?: Map<string, number>;
  firstUnreadSeq: number | null;
  onVisibleSeqChange?: (seq: number) => void;
  onLoadOlder?: () => void;
  hasOlder?: boolean;
  loadingOlder?: boolean;
  onOpenMedia?: (message: WireMessage, item: number) => void;
  onJumpTo?: (messageID: string) => void;
  actions?: MessageActions;
  showNames?: boolean;
  selection?: MessageSelection | null;
  onToggleSelected?: (messageID: string) => void;
  onToggleItem?: (messageID: string, item: number, total: number) => void;
  onSelectMessages?: (selection: MessageSelection | null) => void;
}

interface RowContext {
  selfID: string;
  nameOf: NameOf;
  avatars?: Map<string, string>;
  readSeq: number;
  showNames: boolean;
  menuFor: string | null;
  setMenuFor: (id: string | null) => void;
  onOpenMedia?: (message: WireMessage, item: number) => void;
  onJumpTo?: (messageID: string) => void;
  actions?: MessageActions;
  selection: MessageSelection | null;
  onToggleSelected?: (messageID: string) => void;
  onToggleItem?: (messageID: string, item: number, total: number) => void;
}

interface Edges {
  firstID: string | null;
  lastID: string | null;
  prepended: boolean;
}

const loadOlderThreshold = 400;
const atBottomThreshold = 80;
const flashMs = 1600;
const settleStableFrames = 2;
const settleMaxFrames = 30;
const revealHoldMs = 1500;
const noOutgoing: OutgoingMessage[] = [];

const rowOf = (frame: HTMLElement, messageID: string) =>
  frame.querySelector<HTMLElement>(`[data-id="${CSS.escape(messageID)}"]`);

function lastStartedSeq(item: TranscriptItem, frame: HTMLElement): number {
  const edge = frame.getBoundingClientRect().bottom;
  const rows = frame.querySelector(`[data-item="${CSS.escape(item.key)}"]`)?.querySelectorAll<HTMLElement>("[data-seq]") ?? [];
  for (let index = rows.length - 1; index > 0; index -= 1) {
    if (rows[index].getBoundingClientRect().top < edge) return Number(rows[index].dataset.seq);
  }
  return item.messages[0].seq;
}

export const Transcript = memo(function Transcript({
  ref,
  messages,
  outgoing = noOutgoing,
  connected = false,
  onRetrySend,
  onDismissSend,
  selfID,
  nameOf,
  avatars,
  readSeqs,
  firstUnreadSeq,
  onVisibleSeqChange,
  onLoadOlder,
  hasOlder = false,
  loadingOlder = false,
  onOpenMedia,
  onJumpTo,
  actions,
  showNames = true,
  selection = null,
  onToggleSelected,
  onToggleItem,
  onSelectMessages,
}: TranscriptProps) {
  const viewport = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const list = useRef<VirtualizerHandle>(null);
  const atBottom = useRef(true);
  const olderRequested = useRef(false);
  const olderHeldUntil = useRef(0);
  const userMoved = useRef(false);
  const anchoredKey = useRef<string | null>(null);
  const [settled, setSettled] = useState(false);
  const [away, setAway] = useState(false);
  const [seenSeq, setSeenSeq] = useState(0);
  const [flashID, setFlashID] = useState<string | null>(null);
  const [menuFor, setMenuFor] = useState<string | null>(null);

  const items = useMemo(() => groupTranscript(messages, firstUnreadSeq), [messages, firstUnreadSeq]);

  const pending = useMemo(() => outgoing.map(item => {
    const reply = item.frame.reply_to_id ? messages.find(message => message.id === item.frame.reply_to_id) : undefined;
    const message: WireMessage = {
      id: `pending-${item.frame.client_id}`,
      client_id: item.frame.client_id,
      room_id: item.roomId ?? ("room_id" in item.frame ? item.frame.room_id : ""),
      author_id: selfID,
      body: item.frame.body || (item.frame.upload_ids?.length ? "Attachments" : "Attachment"),
      seq: item.seq ?? 0,
      sent_at: item.createdAt,
      reply_to_id: item.frame.reply_to_id,
      reply_to: reply ? { id: reply.id, author_id: reply.author_id, body: reply.body, kind: reply.kind ?? ContentKind.Text, deleted: isDeleted(reply) } : undefined,
    };
    return { item, message };
  }), [outgoing, messages, selfID]);

  const readSeq = useMemo(() => {
    let max = 0;
    for (const seq of readSeqs?.values() ?? []) max = Math.max(max, seq);
    return max;
  }, [readSeqs]);

  const context = useMemo<RowContext>(
    () => ({
      selfID,
      nameOf,
      avatars,
      readSeq,
      showNames,
      menuFor,
      setMenuFor,
      onOpenMedia,
      onJumpTo,
      actions,
      selection,
      onToggleSelected,
      onToggleItem,
    }),
    [
      selfID,
      nameOf,
      avatars,
      readSeq,
      showNames,
      menuFor,
      onOpenMedia,
      onJumpTo,
      actions,
      selection,
      onToggleSelected,
      onToggleItem,
    ],
  );

  const firstID = messages[0]?.id ?? null;
  const lastID = messages.at(-1)?.id ?? null;
  const hasMessages = lastID !== null || pending.length > 0;
  const [edges, setEdges] = useState<Edges>({ firstID, lastID, prepended: false });
  if (edges.firstID !== firstID || edges.lastID !== lastID) {
    setEdges({
      firstID,
      lastID,
      prepended: edges.firstID !== null && firstID !== edges.firstID && lastID === edges.lastID,
    });
  }

  const noSelect = useCallback(() => {}, []);
  useDragSelect({
    viewport,
    enabled: hasMessages && settled && onSelectMessages !== undefined,
    messages,
    selection,
    onSelect: onSelectMessages ?? noSelect,
  });

  const latest = useRef({ items, hasOlder, loadingOlder, onLoadOlder, onVisibleSeqChange, firstUnreadSeq });
  useLayoutEffect(() => {
    latest.current = { items, hasOlder, loadingOlder, onLoadOlder, onVisibleSeqChange, firstUnreadSeq };
  });

  const pinToBottom = useCallback(() => {
    const element = viewport.current;
    if (element) element.scrollTop = element.scrollHeight;
  }, []);

  const pinnedSends = useRef(new Set<string>());
  const lastPendingID = pending.at(-1)?.message.id;
  const lastPendingClientID = pending.at(-1)?.item.frame.client_id;
  useLayoutEffect(() => {
    if (!lastPendingID) return;
    if (lastPendingClientID) pinnedSends.current.add(lastPendingClientID);
    userMoved.current = true;
    atBottom.current = true;
    pinToBottom();
  }, [lastPendingID, lastPendingClientID, pinToBottom]);

  const measureBottom = useCallback(() => {
    const element = viewport.current;
    if (!element) return;
    atBottom.current = element.scrollHeight - element.scrollTop - element.clientHeight < atBottomThreshold;
    setAway(!atBottom.current);
  }, []);

  const reportVisible = useCallback(() => {
    const handle = list.current;
    const frame = viewport.current;
    const { items: current, onVisibleSeqChange: report } = latest.current;
    if (!handle || !frame || current.length === 0) return;
    let seq: number;
    if (handle.scrollSize <= handle.viewportSize) {
      const last = current[current.length - 1];
      seq = last.messages[last.messages.length - 1].seq;
    } else {
      const index = Math.max(0, Math.min(handle.findItemIndex(handle.scrollOffset + handle.viewportSize - 1), current.length - 1));
      seq = lastStartedSeq(current[index], frame);
    }
    setSeenSeq((previous) => Math.max(previous, seq));
    report?.(seq);
  }, []);

  const loadOlderIfNear = useCallback((offset: number) => {
    const { hasOlder: older, loadingOlder: loading, onLoadOlder: load } = latest.current;
    if (!older || loading || !load || olderRequested.current) return;
    if (performance.now() < olderHeldUntil.current) return;
    if (offset < loadOlderThreshold) {
      olderRequested.current = true;
      load();
    }
  }, []);

  const onScroll = () => {
    const element = viewport.current;
    if (!element || !settled) return;
    measureBottom();
    reportVisible();
    loadOlderIfNear(element.scrollTop);
  };

  const indexOf = useCallback(
    (messageID: string) =>
      latest.current.items.findIndex((item) => item.messages.some((message) => message.id === messageID)),
    [],
  );

  useImperativeHandle(
    ref,
    () => ({
      reveal: (messageID) => {
        if (indexOf(messageID) < 0) return false;
        atBottom.current = false;
        setAway(true);
        olderHeldUntil.current = performance.now() + revealHoldMs;
        let frames = 0;
        let lastOffset = Number.NaN;
        const center = () => {
          const handle = list.current;
          const frame = viewport.current;
          const index = indexOf(messageID);
          if (!handle || !frame || index < 0) return;
          const row = rowOf(frame, messageID);
          const holder = row?.closest<HTMLElement>("[data-item]");
          if (row && holder) {
            const target = row.getBoundingClientRect();
            const offset =
              target.top - holder.getBoundingClientRect().top + target.height / 2 - handle.viewportSize / 2;
            if (Math.abs(offset - lastOffset) < 1) return;
            lastOffset = offset;
            handle.scrollToIndex(index, { align: "start", offset });
          } else {
            handle.scrollToIndex(index, { align: "center" });
          }
          frames += 1;
          if (frames < settleMaxFrames) requestAnimationFrame(center);
        };
        center();
        setFlashID(messageID);
        return true;
      },
      scrollToEnd: () => {
        atBottom.current = true;
        setAway(false);
        pinToBottom();
      },
    }),
    [indexOf, pinToBottom],
  );

  const lastMine = messages.at(-1)?.author_id === selfID;
  const lastClientID = messages.at(-1)?.client_id;
  const shownLastID = useRef<string | null>(null);
  useLayoutEffect(() => {
    if (lastID === null || !settled) return;
    const arrived = shownLastID.current !== null && shownLastID.current !== lastID;
    shownLastID.current = lastID;
    const echoed = lastClientID !== undefined && pinnedSends.current.delete(lastClientID);
    if (atBottom.current || (arrived && lastMine && !echoed)) {
      atBottom.current = true;
      setAway(false);
      pinToBottom();
    }
    reportVisible();
  }, [lastID, lastMine, lastClientID, settled, pinToBottom, reportVisible]);

  useLayoutEffect(() => {
    if (settled || !hasMessages) return;
    pinToBottom();
    let frame = 0;
    let frames = 0;
    let stableFrames = 0;
    let lastHeight = -1;
    const initialUnread = lastPendingID ? null : latest.current.items.find((item) => item.unreadStart)?.key ?? null;
    let anchorUnread = false;
    const place = () => {
      const handle = list.current;
      const element = viewport.current;
      if (!handle || !element) return;
      if (initialUnread !== null && frames === 0) {
        const holder = element.querySelector<HTMLElement>(`[data-item="${CSS.escape(initialUnread)}"]`);
        anchorUnread = !holder || holder.getBoundingClientRect().top < element.getBoundingClientRect().top;
      }
      if (anchorUnread && initialUnread !== null) {
        const index = latest.current.items.findIndex((item) => item.key === initialUnread);
        if (index >= 0) {
          handle.scrollToIndex(index, { align: "start" });
          return;
        }
      }
      pinToBottom();
    };
    const tick = () => {
      const element = viewport.current;
      if (!element) return;
      place();
      stableFrames = element.scrollHeight === lastHeight ? stableFrames + 1 : 0;
      lastHeight = element.scrollHeight;
      frames += 1;
      if (stableFrames >= settleStableFrames || frames >= settleMaxFrames) {
        if (initialUnread !== null) anchoredKey.current = initialUnread;
        atBottom.current = !anchorUnread;
        setAway(anchorUnread);
        setSettled(true);
        reportVisible();
        loadOlderIfNear(element.scrollTop);
        return;
      }
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [settled, hasMessages, lastPendingID, pinToBottom, loadOlderIfNear, reportVisible]);

  const unreadKey = items.find((item) => item.unreadStart)?.key ?? null;
  useEffect(() => {
    const frame = viewport.current;
    if (!frame) return;
    const moved = () => {
      userMoved.current = true;
    };
    const events = ["wheel", "touchstart", "keydown", "pointerdown"] as const;
    for (const type of events) frame.addEventListener(type, moved, { passive: true });
    return () => {
      for (const type of events) frame.removeEventListener(type, moved);
    };
  }, [hasMessages]);

  useLayoutEffect(() => {
    if (!settled || unreadKey === null || userMoved.current || anchoredKey.current !== null) return;
    const element = viewport.current;
    const handle = list.current;
    if (!element || !handle) return;
    anchoredKey.current = unreadKey;
    const holder = element.querySelector<HTMLElement>(`[data-item="${CSS.escape(unreadKey)}"]`);
    if (holder && holder.getBoundingClientRect().top >= element.getBoundingClientRect().top) return;
    let frames = 0;
    let lastHeight = -1;
    let stable = 0;
    let frame = 0;
    const tick = () => {
      const index = latest.current.items.findIndex((item) => item.key === unreadKey);
      if (index < 0 || userMoved.current) return;
      list.current?.scrollToIndex(index, { align: "start" });
      atBottom.current = false;
      setAway(true);
      stable = element.scrollHeight === lastHeight ? stable + 1 : 0;
      lastHeight = element.scrollHeight;
      frames += 1;
      if (stable < settleStableFrames && frames < settleMaxFrames) frame = requestAnimationFrame(tick);
      else reportVisible();
    };
    tick();
    return () => cancelAnimationFrame(frame);
  }, [settled, unreadKey, reportVisible]);

  useEffect(() => {
    const element = content.current;
    const frame = viewport.current;
    if (!element || !frame) return;
    const observer = new ResizeObserver(() => {
      if (atBottom.current) pinToBottom();
    });
    observer.observe(element);
    observer.observe(frame);
    return () => observer.disconnect();
  }, [hasMessages, pinToBottom]);

  useEffect(() => {
    if (loadingOlder) return;
    olderRequested.current = false;
    const handle = list.current;
    if (settled && handle && handle.scrollSize <= handle.viewportSize) loadOlderIfNear(0);
  }, [loadingOlder, firstID, settled, loadOlderIfNear]);

  useEffect(() => {
    if (flashID === null) return;
    const timer = setTimeout(() => setFlashID(null), flashMs);
    return () => clearTimeout(timer);
  }, [flashID]);

  const unseen = useMemo(
    () => (away ? messages.filter((message) => message.seq > seenSeq && message.author_id !== selfID).length : 0),
    [away, messages, seenSeq, selfID],
  );

  if (!hasMessages) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-muted-foreground">Nothing said yet. Go ahead.</p>
      </div>
    );
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <ScrollArea
        className="flex-1"
        viewportProps={{
          ref: viewport,
          role: "log",
          "aria-label": "Messages",
          className: cn("flex flex-col px-1 [overflow-anchor:none]", !settled && "invisible"),
        }}
      >
        <div aria-hidden className="grow" />
        <div ref={content}>
          <Virtualizer ref={list} data={items} shift={edges.prepended} scrollRef={viewport} onScroll={onScroll}>
            {(item) => (
              <TranscriptRow
                key={item.key}
                item={item}
                context={context}
                flashID={flashID !== null && item.messages.some((message) => message.id === flashID) ? flashID : null}
              />
            )}
          </Virtualizer>
          {pending.map(({ item, message }, index) => {
            const at = fromISO(message.sent_at);
            const previous = fromISO(index ? pending[index - 1].message.sent_at : messages.at(-1)?.sent_at ?? "");
            return <div key={message.id}>
              {at && (!previous || at.toDateString() !== previous.toDateString()) ? <DayDivider label={dayLabel(at)} /> : null}
              <MessageRow message={message} context={context} first={index === 0} last={index === pending.length - 1} flash={false} delivery={{ item, connected, retry: () => onRetrySend?.(item.frame.client_id), dismiss: () => onDismissSend?.(item.frame.client_id) }} />
            </div>;
          })}
        </div>
        <div aria-hidden className="h-2 shrink-0" />
      </ScrollArea>

      {loadingOlder ? (
        <div className="pointer-events-none absolute inset-x-0 top-2 flex justify-center">
          <span className="rounded-full border border-border bg-background p-1.5 shadow-sm">
            <Loader2 aria-hidden className="size-4 animate-spin text-muted-foreground" />
          </span>
        </div>
      ) : null}

      {settled && away ? (
        <button
          type="button"
          aria-label={unseen > 0 ? `Scroll to latest, ${unseen} new` : "Scroll to latest"}
          onClick={() => {
            atBottom.current = true;
            setAway(false);
            pinToBottom();
          }}
          className="absolute right-4 bottom-3 flex size-10 items-center justify-center rounded-full border border-border bg-background text-muted-foreground shadow-md transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <ArrowDown aria-hidden className="size-4" />
          {unseen > 0 ? (
            <span className="absolute -top-1.5 -right-1.5 min-w-5 rounded-full bg-signal px-1.5 text-center text-[11px] leading-5 font-medium text-signal-foreground">
              {unseen > 99 ? "99+" : unseen}
            </span>
          ) : null}
        </button>
      ) : null}
    </div>
  );
});

const TranscriptRow = memo(
  function TranscriptRow({
    item,
    context,
    flashID,
  }: {
    item: TranscriptItem;
    context: RowContext;
    flashID: string | null;
  }) {
    const head = item.messages[0];
    const at = item.newDay ? fromISO(head.sent_at) : null;
    return (
      <div data-item={item.key}>
        {at ? <DayDivider label={dayLabel(at)} /> : null}
        {item.unreadStart ? (
          <div role="separator" className="my-2 flex items-center gap-2 px-4 text-xs font-medium text-signal">
            <span className="h-px flex-1 bg-signal/30" />
            Unread messages
            <span className="h-px flex-1 bg-signal/30" />
          </div>
        ) : null}
        {head.kind === ContentKind.System ? (
          <div data-id={head.id} data-seq={head.seq} className={cn(context.selection !== null && "pl-9")}>
            <SystemLine text={systemTextOf(head, context.nameOf, context.selfID)} />
          </div>
        ) : (
          <Series item={item} context={context} flashID={flashID} />
        )}
      </div>
    );
  },
  (previous, next) =>
    previous.context === next.context && previous.flashID === next.flashID && sameItem(previous.item, next.item),
);

function Series({ item, context, flashID }: { item: TranscriptItem; context: RowContext; flashID: string | null }) {
  const { messages } = item;
  const author = messages[0].author_id;
  const mine = author === context.selfID;
  const name = context.nameOf(author);
  return (
    <div className="relative pt-1">
      {messages.map((message, index) => (
        <MessageRow
          key={message.id}
          message={message}
          context={context}
          first={index === 0}
          last={index === messages.length - 1}
          flash={flashID === message.id}
        />
      ))}
      {mine || !context.showNames ? null : (
        <div
          aria-hidden
          data-message-avatar
          className={cn(
            "pointer-events-none absolute top-1 bottom-0 flex w-7 flex-col justify-end",
            context.selection !== null ? "left-11" : "left-2",
          )}
        >
          <Avatar
            src={context.avatars?.get(author)}
            alt=""
            fallback={name.slice(0, 1).toUpperCase()}
            className="sticky bottom-1 size-7"
            sizes="28px"
          />
        </div>
      )}
    </div>
  );
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

function MessageText({ text }: { text: string }) {
  const segments = useMemo(() => splitLinks(text), [text]);
  return (
    <>
      {segments.map((segment, index) =>
        segment.href ? (
          <a
            key={index}
            href={segment.href}
            target="_blank"
            rel="noopener noreferrer"
            className={cn(
              "break-all underline underline-offset-2",
              "text-signal decoration-signal/70",
            )}
          >
            {segment.text}
          </a>
        ) : (
          <span key={index}>{segment.text}</span>
        ),
      )}
    </>
  );
}

const MessageRow = memo(function MessageRow({
  message,
  context,
  first,
  last,
  flash,
  delivery,
}: {
  message: WireMessage;
  context: RowContext;
  first: boolean;
  last: boolean;
  flash: boolean;
  delivery?: { item: OutgoingMessage; connected: boolean; retry: () => void; dismiss: () => void };
}) {
  const { selfID, nameOf, actions, menuFor, setMenuFor, onOpenMedia, onJumpTo, selection, onToggleSelected, onToggleItem } =
    context;
  const selecting = selection !== null;
  const selectable = !delivery && selecting && canSelect(message);
  const selected = selectable && selection.has(message.id);
  const partial = selected && selection.get(message.id) !== null;
  const mine = message.author_id === selfID;
  const name = nameOf(message.author_id);
  const deleted = isDeleted(message);
  const read = mine && context.readSeq >= message.seq;
  const content = contentOf(message);
  const isMedia = !deleted && content.kind !== ContentKind.Text;
  const visual =
    isMedia && (
      content.kind === ContentKind.Image || content.kind === ContentKind.Video ||
      (content.kind === ContentKind.Album && content.items.some((item) => item.kind !== ContentKind.File))
    );
  const caption = message.body.trim();
  const mediaWidth = visual
    ? content.kind === ContentKind.Image
      ? mediaBoxSize(content.image.width, content.image.height, "photo").width
      : content.kind === ContentKind.Video
        ? mediaBoxSize(content.video.width, content.video.height).width
        : 280
    : undefined;
  const padded = cn(visual && "px-2");
  const hasText = deleted || !isMedia || caption !== "";
  const menuOpen = menuFor === message.id;
  const onMenuOpenChange = (open: boolean) => setMenuFor(open ? message.id : null);
  const interactive = !delivery && actions !== undefined && !deleted && !selecting;
  const swipe = useSwipeToReply(() => actions?.onReply(message), interactive);
  const longPress = useLongPress(() => onMenuOpenChange(true), interactive);

  const meta = (
    <span
      className={cn(
        "ml-auto flex shrink-0 items-center gap-1",
        hasText && "leading-none",
        "text-muted-foreground",
      )}
    >
      {message.edited_at && !deleted ? <span className="text-[10px]">edited</span> : null}
      <time className="text-[10px]" dateTime={message.sent_at}>
        {formatTime(message.sent_at)}
      </time>
      {mine && delivery && delivery.item.status !== "sent" ? (
        delivery.item.status === "failed"
          ? <CircleAlert role="img" aria-label="Not sent" className="size-3.5 shrink-0 text-destructive" />
          : <Clock role="img" aria-label={delivery.item.status === "sending" ? "Sending" : "Delivery unconfirmed"} className="size-3.5 shrink-0" />
      ) : mine ? (
        <>
          <span className="sr-only">{read ? "Read" : "Sent"}</span>
          {read ? <CheckCheck aria-hidden className="size-3.5 shrink-0 text-signal" /> : <Check aria-hidden className="size-3.5 shrink-0" />}
        </>
      ) : null}
    </span>
  );

  return (
    <div
      data-id={message.id}
      data-seq={delivery ? undefined : message.seq}
      {...swipe.handlers}
      {...longPress}
      className={cn(
        "group relative flex items-end gap-2 px-2 py-0.5 transition-colors",
        mine ? "flex-row-reverse" : "flex-row",
        !mine && context.showNames && "pl-11",
        selecting && (!mine && context.showNames ? "pl-20" : "pl-11"),
        selected && "bg-signal/10",
        "pointer-coarse:select-none pointer-coarse:[-webkit-touch-callout:none]",
      )}
      onContextMenu={(event) => {
        if (!interactive) return;
        event.preventDefault();
        onMenuOpenChange(true);
      }}
      onKeyDown={(event) => {
        if (!interactive || menuOpen || !(event.key === "ContextMenu" || (event.key === "F10" && event.shiftKey))) return;
        event.preventDefault();
        onMenuOpenChange(true);
      }}
    >
      {swipe.shift > 0 ? (
        <Reply
          aria-hidden
          className="absolute top-1/2 right-3 size-4 -translate-y-1/2 text-muted-foreground"
          style={{ opacity: Math.min(1, swipe.shift / 56) }}
        />
      ) : null}
      <article
        style={{
          width: mediaWidth === undefined ? undefined : mediaWidth + 10,
          transform: swipe.shift > 0 ? `translateX(${-swipe.shift}px)` : undefined,
        }}
        aria-label={mine ? "Your message" : `Message from ${name}`}
        data-voice={content.kind === ContentKind.Voice && !caption && !deleted || undefined}
        className={cn(
          "max-w-[86%] min-w-0 rounded-2xl border border-border/65 text-foreground transition-shadow duration-300 sm:max-w-[75%]",
          visual ? "p-1" : "px-3 py-2",
          mine ? "bg-message-outgoing" : "bg-message-incoming",
          last && (mine ? "rounded-br-sm" : "rounded-bl-sm"),
          flash && "ring-2 ring-signal/60 ring-offset-2 ring-offset-background",
        )}
      >
        {mine || !context.showNames || !first ? null : (
          <p className={cn("text-xs font-medium text-muted-foreground", visual && "px-2 pt-1 pb-1")}>{name}</p>
        )}

        {deleted ? null : (
          <>
            {message.forwarded_from_id ? (
              <p className={cn("text-xs text-signal", padded)}>
                Forwarded from <span className="font-medium">{nameOf(message.forwarded_from_author_id)}</span>
              </p>
            ) : null}

            {message.reply_to ? (
              <button
                type="button"
                onClick={() => message.reply_to && onJumpTo?.(message.reply_to.id)}
                aria-label={`Replying to ${nameOf(message.reply_to.author_id)}`}
                className={cn(
                  "my-1 block w-full min-w-0 rounded-md border-l-2 px-2 py-1 text-left",
                  visual && "mx-1 w-[calc(100%-0.5rem)]",
                  "border-signal bg-background/60",
                )}
              >
                <span className="block truncate text-xs font-medium">{nameOf(message.reply_to.author_id)}</span>
                <span
                  className={cn(
                    "block truncate text-xs",
                    "text-muted-foreground",
                    message.reply_to.deleted && "italic",
                  )}
                >
                  {quoteOf(message.reply_to)}
                </span>
              </button>
            ) : null}

            {isMedia ? (
              <MessageMedia
                content={content}
                caption={caption}
                mine={mine}
                onOpen={(item) => onOpenMedia?.(message, item)}
                picked={selectable ? (item) => isPicked(selection, message.id, item) : undefined}
                onTogglePicked={
                  selectable && content.kind === ContentKind.Album
                    ? (item) => onToggleItem?.(message.id, item, content.items.length)
                    : undefined
                }
              />
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
                  "text-muted-foreground",
                )}
              >
                <Ban aria-hidden className="size-3.5 shrink-0" />
                Message deleted
              </p>
            ) : (
              <p
                className={cn(
                  "min-w-0 whitespace-pre-wrap text-sm [overflow-wrap:anywhere]",
                  message.body.trim().includes("\n") && "basis-full",
                )}
              >
                <MessageText text={message.body} />
              </p>
            )}
            {meta}
          </div>
        ) : (
          <div className={cn("mt-0.5 flex", visual && "px-2 pb-1")}>{meta}</div>
        )}
        {delivery && (delivery.item.status === "unconfirmed" || delivery.item.status === "failed") ? (
          <div className="mt-1.5 border-t border-border/40 pt-1.5 text-xs">
            <p role="status" className={delivery.item.status === "failed" ? "text-destructive" : "text-muted-foreground"}>{delivery.item.reason || "Delivery unconfirmed"}</p>
            <div className="mt-1 flex gap-3">
              {delivery.item.status === "unconfirmed" ? <button type="button" className="min-h-8 text-signal disabled:opacity-50" disabled={!delivery.connected} onClick={delivery.retry}>Retry</button> : null}
              <button type="button" className="min-h-8 text-muted-foreground" onClick={delivery.dismiss}>Dismiss</button>
            </div>
          </div>
        ) : null}
      </article>

      {interactive ? (
        <MessageMenu message={message} mine={mine} open={menuOpen} onOpenChange={onMenuOpenChange} actions={actions} />
      ) : null}

      {selectable ? (
        <button
          type="button"
          role="checkbox"
          aria-checked={partial ? "mixed" : selected}
          aria-label={`Select message from ${mine ? "you" : name}`}
          onClick={() => onToggleSelected?.(message.id)}
          className={cn(
            "absolute z-10 flex items-center pl-3 outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset",
            content.kind === ContentKind.Album ? "inset-y-0 left-0 w-10" : "inset-0",
          )}
        >
          {selected ? (
            <CheckCircle2 aria-hidden className="size-5 fill-signal text-signal-foreground" />
          ) : (
            <Circle aria-hidden className="size-5 text-muted-foreground" />
          )}
        </button>
      ) : null}
    </div>
  );
});

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
            aria-keyshortcuts="Shift+F10"
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
        {contentOf(message).kind === ContentKind.Image ? (
          <DropdownMenuItem onClick={() => actions.onCopyImage(message)}>
            <ImageIcon />
            Copy image
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuItem onClick={() => actions.onSelect(message)}>
          <ListChecks />
          Select
        </DropdownMenuItem>
        {mine ? (
          <>
            <DropdownMenuItem onClick={() => actions.onReaders(message)}>
              <Eye />
              Read by
            </DropdownMenuItem>
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
  const at = fromISO(iso);
  return at ? clockTime(at) : "";
}
