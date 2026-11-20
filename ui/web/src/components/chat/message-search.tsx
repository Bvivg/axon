"use client";

import { Loader2, X } from "lucide-react";
import { useState } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { NameOf } from "@/lib/chat/content";
import { snippetOf } from "@/lib/chat/search";
import { describe } from "@/lib/errors";
import { fromISO, shortWhen } from "@/lib/format";
import { useDebouncedValue } from "@/lib/hooks/use-debounced-value";
import { minSearchQueryLength, useSearchMessages } from "@/lib/query/chat";
import type { WireMessage } from "@/lib/ws/protocol";

const searchDelayMs = 300;

export function MessageSearch({
  roomID,
  nameOf,
  onPick,
  onClose,
}: {
  roomID: string;
  nameOf: NameOf;
  onPick: (message: WireMessage) => void;
  onClose: () => void;
}) {
  const [query, setQuery] = useState("");
  const debounced = useDebouncedValue(query, searchDelayMs).trim();
  const search = useSearchMessages(roomID, debounced);

  const active = [...debounced].length >= minSearchQueryLength;
  const results = active ? (search.data ?? []) : [];

  return (
    <>
      <div className="flex h-10 min-w-0 flex-1 items-center gap-2">
        <Input
          autoFocus
          aria-label="Search messages"
          placeholder="Search messages"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Escape") onClose();
          }}
          className="flex-1"
        />
        {search.isFetching ? (
          <Loader2 aria-label="Searching" className="size-4 shrink-0 animate-spin text-muted-foreground" />
        ) : null}
        <Button variant="ghost" size="icon" aria-label="Close search" onClick={onClose}>
          <X />
        </Button>
      </div>

      {active && (search.data || search.isError) ? (
        <div className="absolute inset-x-0 top-full z-30 overflow-hidden rounded-b-lg border border-t-0 border-border bg-popover text-popover-foreground shadow-md">
          {search.isError ? (
            <Alert className="m-2">{describe(search.error)}</Alert>
          ) : search.isSuccess && results.length === 0 ? (
            <p className="px-4 py-6 text-center text-sm text-muted-foreground">No messages found</p>
          ) : (
            <ScrollArea className="max-h-72">
              <ul aria-label="Search results" className="divide-y divide-border">
                {results.map((message) => (
                  <li key={message.id}>
                    <button
                      type="button"
                      onClick={() => onPick(message)}
                      className="flex w-full flex-col gap-0.5 px-4 py-2 text-left text-sm transition-colors hover:bg-muted"
                    >
                      <span className="flex items-baseline gap-2">
                        <span className="min-w-0 flex-1 truncate font-medium">{nameOf(message.author_id)}</span>
                        <span className="shrink-0 text-xs text-muted-foreground">{shortWhen(fromISO(message.sent_at))}</span>
                      </span>
                      <span className="line-clamp-2 break-words text-muted-foreground">
                        <Snippet body={message.body} query={debounced} />
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </ScrollArea>
          )}
        </div>
      ) : null}
    </>
  );
}

function Snippet({ body, query }: { body: string; query: string }) {
  return snippetOf(body, query).map((part, index) =>
    part.match ? (
      <mark key={index} className="rounded-sm bg-signal/25 px-0.5 text-foreground">
        {part.text}
      </mark>
    ) : (
      <span key={index}>{part.text}</span>
    ),
  );
}
