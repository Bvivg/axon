import { ContentKind, contentOf, isDeleted, previewOf, type NameOf } from "@/lib/chat/content";
import { clockTime, dayLabel, fromISO } from "@/lib/format";
import type { WireMessage } from "@/lib/ws/protocol";

export type MessageSelection = ReadonlyMap<string, ReadonlySet<number> | null>;

export interface ForwardRequest {
  messageIds: string[];
  parts: { messageId: string; positions: number[] }[];
}

export function canSelect(message: WireMessage): boolean {
  return message.kind !== ContentKind.System && !isDeleted(message);
}

export function itemsOf(message: WireMessage): number {
  const content = contentOf(message);
  return content.kind === ContentKind.Album ? content.items.length : 0;
}

const settled = (next: Map<string, ReadonlySet<number> | null>): MessageSelection | null =>
  next.size === 0 ? null : next;

export function toggleMessage(selection: MessageSelection | null, messageID: string): MessageSelection | null {
  const next = new Map(selection ?? []);
  if (next.get(messageID) === null) next.delete(messageID);
  else next.set(messageID, null);
  return settled(next);
}

export function toggleItem(
  selection: MessageSelection | null,
  messageID: string,
  item: number,
  total: number,
): MessageSelection | null {
  const next = new Map(selection ?? []);
  const current = next.get(messageID);
  const picked = new Set(current === null ? Array.from({ length: total }, (_, i) => i) : (current ?? []));
  if (picked.has(item)) picked.delete(item);
  else picked.add(item);
  if (picked.size === 0) next.delete(messageID);
  else next.set(messageID, picked.size === total ? null : picked);
  return settled(next);
}

export function isPicked(selection: MessageSelection | null, messageID: string, item: number): boolean {
  const entry = selection?.get(messageID);
  return entry === null || entry?.has(item) === true;
}

export function selectWhole(selection: MessageSelection | null, ids: readonly string[]): MessageSelection | null {
  const next = new Map(selection ?? []);
  for (const id of ids) next.set(id, null);
  return settled(next);
}

export function selectableBetween(messages: readonly WireMessage[], fromSeq: number, toSeq: number): string[] {
  const low = Math.min(fromSeq, toSeq);
  const high = Math.max(fromSeq, toSeq);
  return messages
    .filter((message) => message.seq >= low && message.seq <= high && canSelect(message))
    .map((message) => message.id);
}

export function selectedMessages(selection: MessageSelection | null, messages: readonly WireMessage[]): WireMessage[] {
  return selection ? messages.filter((message) => selection.has(message.id) && canSelect(message)) : [];
}

export function selectedCount(selection: MessageSelection | null, messages: readonly WireMessage[]): number {
  return selectedMessages(selection, messages).reduce((total, message) => {
    const entry = selection?.get(message.id);
    if (entry) return total + entry.size;
    return total + Math.max(1, itemsOf(message));
  }, 0);
}

export function forwardRequest(selection: MessageSelection | null, messages: readonly WireMessage[]): ForwardRequest {
  const chosen = selectedMessages(selection, messages);
  const parts = chosen.flatMap((message) => {
    const entry = selection?.get(message.id);
    return entry ? [{ messageId: message.id, positions: [...entry].sort((a, b) => a - b) }] : [];
  });
  return { messageIds: chosen.map((message) => message.id), parts };
}

export function selectionText(messages: readonly WireMessage[], nameOf: NameOf): string {
  if (messages.length === 1) return previewOf(messages[0]);
  return messages
    .map((message) => {
      const at = fromISO(message.sent_at);
      const stamp = at ? `${dayLabel(at)} ${clockTime(at)}` : "";
      return `${nameOf(message.author_id)}, ${stamp}\n${previewOf(message)}`;
    })
    .join("\n\n");
}
