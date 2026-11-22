"use client";

import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, ArrowUp, ArrowUpDown, SearchX, type LucideIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, type MouseEvent } from "react";

import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

export enum SortDirection {
  Ascending = "ascending",
  Descending = "descending",
}

export interface DataTableSort {
  column: string;
  direction: SortDirection;
  sortable: string[];
  onSort: (column: string) => void;
}

export interface DataTableSelection<T> {
  selected: ReadonlySet<string>;
  onChange: (next: Set<string>) => void;
  selectable?: (row: T) => boolean;
  limit?: number;
}

export interface DataTableProps<T> {
  label: string;
  columns: ColumnDef<T, unknown>[];
  rows: T[];
  rowKey: (row: T) => string;
  rowHref?: (row: T) => string;
  status: "pending" | "error" | "success";
  error?: unknown;
  onRetry?: () => void;
  filtered: boolean;
  onClearFilters?: () => void;
  empty: { icon: LucideIcon; title: string };
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  sort?: DataTableSort;
  rowHeight?: number;
  selection?: DataTableSelection<T>;
}

function SortIcon({ direction }: { direction?: SortDirection }) {
  if (direction === SortDirection.Ascending) return <ArrowUp aria-hidden className="size-3.5" />;
  if (direction === SortDirection.Descending) return <ArrowDown aria-hidden className="size-3.5" />;
  return <ArrowUpDown aria-hidden className="size-3.5 opacity-50" />;
}

export function DataTable<T>({
  label,
  columns,
  rows,
  rowKey,
  rowHref,
  status,
  error,
  onRetry,
  filtered,
  onClearFilters,
  empty,
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  sort,
  rowHeight = 52,
  selection,
}: DataTableProps<T>) {
  const router = useRouter();
  const viewport = useRef<HTMLDivElement>(null);
  const allColumns = useMemo<ColumnDef<T, unknown>[]>(() => {
    if (!selection) return columns;
    const limit = selection.limit ?? Infinity;
    const eligible = rows.filter((row) => selection.selectable?.(row) ?? true).map(rowKey);
    const picked = eligible.filter((key) => selection.selected.has(key)).length;
    const toggle = (key: string, on: boolean) => {
      const next = new Set(selection.selected);
      if (on && next.size < limit) next.add(key);
      else next.delete(key);
      selection.onChange(next);
    };
    return [{
      id: "select",
      size: 36,
      header: () => (
        <input
          type="checkbox"
          aria-label={`Select all loaded ${label.toLowerCase()}`}
          className="size-4 accent-signal"
          checked={eligible.length > 0 && picked === Math.min(eligible.length, limit)}
          ref={(node) => { if (node) node.indeterminate = picked > 0 && picked < Math.min(eligible.length, limit); }}
          disabled={eligible.length === 0}
          onChange={(event) => selection.onChange(new Set(event.target.checked ? eligible.slice(0, limit) : []))}
        />
      ),
      cell: ({ row }) => {
        const key = rowKey(row.original);
        const allowed = selection.selectable?.(row.original) ?? true;
        return (
          <input
            type="checkbox"
            aria-label="Select row"
            className="size-4 accent-signal"
            checked={selection.selected.has(key)}
            disabled={!allowed || (!selection.selected.has(key) && selection.selected.size >= limit)}
            onChange={(event) => toggle(key, event.target.checked)}
          />
        );
      },
    }, ...columns];
  }, [columns, selection, rows, rowKey, label]);
  const table = useReactTable({ data: rows, columns: allColumns, getCoreRowModel: getCoreRowModel(), getRowId: rowKey, manualSorting: true });
  const virtual = useVirtualizer({ count: rows.length, getScrollElement: () => viewport.current, estimateSize: () => rowHeight, overscan: 8 });
  const items = virtual.getVirtualItems();
  const last = items.at(-1)?.index ?? -1;

  const failed = status === "error";

  useEffect(() => {
    if (hasMore && !failed && !loadingMore && onLoadMore && last >= rows.length - 6) onLoadMore();
  }, [hasMore, failed, loadingMore, onLoadMore, last, rows.length]);

  if (failed && rows.length === 0) {
    return <div className="p-4"><ErrorCard title={`Could not load ${label.toLowerCase()}`} error={error} onRetry={onRetry} /></div>;
  }

  if (status === "pending") {
    return (
      <div role="status" aria-label={`Loading ${label.toLowerCase()}`} className="flex flex-col gap-2 p-4">
        {Array.from({ length: 6 }, (_, i) => <Skeleton key={i} className="h-10 w-full" />)}
      </div>
    );
  }

  if (rows.length === 0) {
    return filtered ? (
      <EmptyState icon={SearchX} title="Nothing matches these filters">
        {onClearFilters ? <Button variant="outline" size="sm" onClick={onClearFilters}>Clear filters</Button> : null}
      </EmptyState>
    ) : (
      <EmptyState icon={empty.icon} title={empty.title} />
    );
  }

  const top = items[0]?.start ?? 0;
  const bottom = virtual.getTotalSize() - (items.at(-1)?.end ?? 0);
  const width = table.getVisibleLeafColumns().length;

  const open = (event: MouseEvent<HTMLTableRowElement>, href?: string) => {
    if (!href || event.defaultPrevented) return;
    const target = event.target as HTMLElement;
    if (target.closest("a,button,input,textarea,select,[role=menuitem]")) return;
    if (window.getSelection()?.toString()) return;
    router.push(href);
  };

  return (
    <ScrollArea className="min-h-0 flex-1" orientation="both" viewportProps={{ ref: viewport }}>
      <table aria-label={label} aria-rowcount={hasMore ? -1 : rows.length + 1} className="w-full min-w-[44rem] border-collapse text-left text-sm">
        <thead className="sticky top-0 z-10 bg-surface text-xs text-muted-foreground shadow-[inset_0_-1px_0_var(--color-line)]">
          {table.getHeaderGroups().map((group) => (
            <tr key={group.id} aria-rowindex={1}>
              {group.headers.map((header) => {
                const sortable = sort?.sortable.includes(header.column.id);
                const direction = sort?.column === header.column.id ? sort.direction : undefined;
                return (
                  <th key={header.id} scope="col" aria-sort={sortable ? direction ?? "none" : undefined} className="h-10 px-3 font-medium whitespace-nowrap first:pl-4 last:pr-4" style={{ width: header.getSize() !== 150 ? header.getSize() : undefined }}>
                    {sortable ? (
                      <button type="button" className="-mx-1 inline-flex items-center gap-1 rounded px-1 py-0.5 hover:text-foreground focus-visible:outline-2 focus-visible:outline-signal/50" onClick={() => sort?.onSort(header.column.id)}>
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        <SortIcon direction={direction} />
                      </button>
                    ) : flexRender(header.column.columnDef.header, header.getContext())}
                  </th>
                );
              })}
            </tr>
          ))}
        </thead>
        <tbody>
          {top > 0 ? <tr aria-hidden><td colSpan={width} style={{ height: top }} /></tr> : null}
          {items.map((item) => {
            const row = table.getRowModel().rows[item.index];
            const href = rowHref?.(row.original);
            return (
              <tr
                key={row.id}
                data-index={item.index}
                aria-rowindex={item.index + 2}
                ref={virtual.measureElement}
                onClick={(event) => open(event, href)}
                className={cn("border-b border-line align-middle", href && "cursor-pointer hover:bg-accent/60")}
              >
                {row.getVisibleCells().map((cell) => (
                  <td key={cell.id} className="px-3 py-2.5 first:pl-4 last:pr-4">{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>
                ))}
              </tr>
            );
          })}
          {bottom > 0 ? <tr aria-hidden><td colSpan={width} style={{ height: bottom }} /></tr> : null}
        </tbody>
      </table>
      {failed ? (
        <div role="alert" className="flex items-center justify-center gap-3 p-3 text-sm text-muted-foreground">
          <span>Could not load more {label.toLowerCase()}</span>
          <Button variant="outline" size="sm" onClick={hasMore ? onLoadMore : onRetry} disabled={loadingMore} aria-busy={loadingMore}>Retry</Button>
        </div>
      ) : hasMore ? (
        <div className="flex justify-center p-3">
          <Button variant="outline" size="sm" onClick={onLoadMore} disabled={loadingMore} aria-busy={loadingMore}>{loadingMore ? "Loading…" : "Load more"}</Button>
        </div>
      ) : null}
    </ScrollArea>
  );
}
