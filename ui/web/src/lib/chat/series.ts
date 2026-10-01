import { ContentKind } from "@/lib/chat/content";
import type { WireMessage } from "@/lib/ws/protocol";

export const seriesGapMs = 5 * 60 * 1000;

export const seriesMaxMessages = 30;

export interface TranscriptItem {
  key: string;
  messages: WireMessage[];
  newDay: boolean;
  unreadStart: boolean;
}

interface Stamp {
  time: number;
  day: string;
}

const stamps = new WeakMap<WireMessage, Stamp>();

function stampOf(message: WireMessage): Stamp {
  let stamp = stamps.get(message);
  if (stamp === undefined) {
    const at = new Date(message.sent_at);
    const time = at.getTime();
    stamp = Number.isNaN(time)
      ? { time: 0, day: "" }
      : { time, day: `${at.getFullYear()}-${at.getMonth()}-${at.getDate()}` };
    stamps.set(message, stamp);
  }
  return stamp;
}

function isSystem(message: WireMessage): boolean {
  return message.kind === ContentKind.System;
}

function continues(message: WireMessage, previous: WireMessage): boolean {
  if (isSystem(previous) || isSystem(message)) return false;
  if (previous.author_id !== message.author_id) return false;
  const gap = stampOf(message).time - stampOf(previous).time;
  return gap >= 0 && gap < seriesGapMs && stampOf(message).day === stampOf(previous).day;
}

export function sameItem(a: TranscriptItem, b: TranscriptItem): boolean {
  return (
    a === b ||
    (a.key === b.key &&
      a.newDay === b.newDay &&
      a.unreadStart === b.unreadStart &&
      a.messages.length === b.messages.length &&
      a.messages.every((message, index) => message === b.messages[index]))
  );
}

export function groupTranscript(
  messages: readonly WireMessage[],
  firstUnreadSeq: number | null,
): TranscriptItem[] {
  const items: TranscriptItem[] = [];
  let previous: WireMessage | undefined;
  for (const message of messages) {
    const unreadStart = firstUnreadSeq !== null && message.seq === firstUnreadSeq;
    const current = items.at(-1);
    const newDay = previous === undefined || stampOf(message).day !== stampOf(previous).day;
    if (
      current &&
      previous &&
      !unreadStart &&
      !newDay &&
      current.messages.length < seriesMaxMessages &&
      continues(message, previous)
    ) {
      current.messages.push(message);
    } else {
      items.push({ key: message.id, messages: [message], newDay, unreadStart });
    }
    previous = message;
  }
  return items;
}

export function firstUnreadOf(
  messages: readonly WireMessage[],
  readSeq: number,
  selfID: string,
): number | null {
  const first = messages.find((message) => message.seq > readSeq && message.author_id !== selfID);
  return first?.seq ?? null;
}
