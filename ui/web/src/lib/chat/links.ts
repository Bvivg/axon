import { find } from "linkifyjs";

const safeHref = /^(https?:|mailto:)/i;

export interface TextSegment {
  text: string;
  href: string | null;
}

export function splitLinks(text: string): TextSegment[] {
  const segments: TextSegment[] = [];
  let cursor = 0;
  for (const link of find(text, { defaultProtocol: "https" })) {
    if (!safeHref.test(link.href) || link.start < cursor) continue;
    if (link.start > cursor) {
      segments.push({ text: text.slice(cursor, link.start), href: null });
    }
    segments.push({ text: link.value, href: link.href });
    cursor = link.end;
  }
  if (cursor < text.length) segments.push({ text: text.slice(cursor), href: null });
  return segments;
}
