import { ContentKind, isDeleted, previewOf, type NameOf } from "@/lib/chat/content";
import { clockTime, dayLabel, fromISO } from "@/lib/format";
import type { WireMessage } from "@/lib/ws/protocol";

export type MessageSelection = ReadonlySet<string>;

export function canSelect(message: WireMessage): boolean {
  return message.kind !== ContentKind.System && !isDeleted(message);
}

const settled = (next: Set<string>): MessageSelection | null => (next.size === 0 ? null : next);

export function toggleMessage(selection: MessageSelection | null, messageID: string): MessageSelection | null {
  const next = new Set(selection ?? []);
  if (next.has(messageID)) next.delete(messageID);
  else next.add(messageID);
  return settled(next);
}

export function selectWhole(selection: MessageSelection | null, ids: readonly string[]): MessageSelection | null {
  const next = new Set(selection ?? []);
  for (const id of ids) next.add(id);
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
