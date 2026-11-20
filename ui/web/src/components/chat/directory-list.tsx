"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { useEffect, useRef, type ReactNode } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingDots } from "@/components/ui/status";
import { describe } from "@/lib/errors";

interface DirectorySection {
  label: string;
  title: string;
  rows: { id: string; content: ReactNode; height: number }[];
}

export function DirectoryList({ sections, search, empty, label, loading = false, rooms, className }: {
  sections: DirectorySection[];
  search: {
    hasNextPage: boolean;
    isFetching: boolean;
    isError: boolean;
    isFetchNextPageError: boolean;
    error: unknown;
    fetchNextPage: () => Promise<unknown>;
    refetch: () => Promise<unknown>;
  };
  empty: string;
  label: string;
  loading?: boolean;
  rooms?: { error: unknown; isFetching: boolean; refetch: () => Promise<unknown> };
  className?: string;
}) {
  const viewport = useRef<HTMLDivElement>(null);
  let offset = 0;
  const groups = sections.filter((section) => section.rows.length > 0).map((section) => {
    const rows = [
      { id: `${section.label}-heading`, content: <h2 className="eyebrow flex h-8 items-center px-4">{section.title}</h2>, height: 32 },
      ...section.rows,
    ];
    const start = offset;
    offset += rows.reduce((sum, row) => sum + row.height, 0);
    return { ...section, rows, start, height: offset - start };
  });
  const rows = groups.flatMap((group) => group.rows);
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => viewport.current,
    getItemKey: (index) => rows[index].id,
    estimateSize: (index) => rows[index].height,
    overscan: 5,
    useFlushSync: false,
  });
  const visible = virtualizer.getVirtualItems();
  const last = visible.at(-1)?.index ?? 0;
  const { hasNextPage, isFetching, isError, fetchNextPage } = search;

  useEffect(() => {
    if (last >= rows.length - 4 && hasNextPage && !isFetching && !isError && !loading) {
      void fetchNextPage();
    }
  }, [last, rows.length, hasNextPage, isFetching, isError, fetchNextPage, loading]);

  return (
    <ScrollArea className={className ?? "flex-1"} viewportProps={{ ref: viewport, "aria-label": `${label} scroll` }} aria-label={label}>
      {groups.map((group) => (
        <section key={group.label} aria-label={group.label} className="relative" style={{ height: group.height }}>
          {visible.filter((row) => row.start >= group.start && row.start < group.start + group.height).map((row) => (
            <div key={row.key} className="absolute top-0 left-0 w-full" style={{ height: row.size, transform: `translateY(${row.start - group.start}px)` }}>
              {rows[row.index].content}
            </div>
          ))}
        </section>
      ))}
      {loading || isFetching ? (
        <p role="status" className="flex items-center justify-center gap-2 p-6 text-sm text-muted-foreground">
          <LoadingDots aria-hidden="true" /> {rows.length ? "Updating results…" : "Searching…"}
        </p>
      ) : null}
      {rooms?.error ? (
        <div className="space-y-3 px-4 py-3">
          <Alert><p className="font-medium">Could not load chats.</p><p>{describe(rooms.error)}</p></Alert>
          <Button variant="outline" className="min-h-11" disabled={rooms.isFetching} onClick={() => void rooms.refetch()}>Retry chats</Button>
        </div>
      ) : null}
      {search.error ? (
        <div className="space-y-3 px-4 py-3">
          <Alert><p className="font-medium">Could not load people.</p><p>{describe(search.error)}</p></Alert>
          <Button variant="outline" className="min-h-11" disabled={isFetching} onClick={() => void (search.isFetchNextPageError ? fetchNextPage() : search.refetch())}>Retry search</Button>
        </div>
      ) : null}
      {!loading && !isFetching && !rooms?.error && !search.error && !hasNextPage && rows.length === 0 ? <p role="status" className="px-4 py-10 text-center text-sm text-muted-foreground">{empty}</p> : null}
    </ScrollArea>
  );
}
