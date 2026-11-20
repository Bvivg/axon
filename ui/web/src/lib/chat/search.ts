export interface SnippetPart {
  text: string;
  match: boolean;
}

const leadBeforeMatch = 24;
const ellipsis = "…";

export function snippetOf(body: string, query: string): SnippetPart[] {
  const text = body.replace(/\s+/g, " ").trim();
  const needle = query.trim();
  if (needle === "") return [{ text, match: false }];

  const pattern = new RegExp(needle.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "giu");
  const matches = [...text.matchAll(pattern)];
  if (matches.length === 0) return [{ text, match: false }];

  const first = matches[0].index;
  const start = first > leadBeforeMatch ? text.lastIndexOf(" ", first - leadBeforeMatch) + 1 : 0;
  const parts: SnippetPart[] = [];
  let from = start;
  for (const match of matches) {
    const before = text.slice(from, match.index);
    parts.push({ text: from === start && start > 0 ? ellipsis + before : before, match: false });
    parts.push({ text: match[0], match: true });
    from = match.index + match[0].length;
  }
  parts.push({ text: text.slice(from), match: false });
  return parts.filter((part) => part.text !== "");
}
