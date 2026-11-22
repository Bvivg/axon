"use client";

import { flexRender, getCoreRowModel, useReactTable, type ColumnDef, type Header, type Row, type RowData } from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, ArrowUp, ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight, ChevronsUpDown, ListFilter, MoreHorizontal, SearchX, X, type LucideIcon } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, type MouseEvent, type ReactNode } from "react";

import { FilterEditor } from "@/components/admin/filter-editor";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { Popover, PopoverClose, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { ListMode, type ListState } from "@/lib/admin/list";
import { FilterKind, pageCount, pageSizes, pageWindow, type FilterDef, type Option } from "@/lib/admin/list-query";
import { useIsDesktop } from "@/lib/admin/media";
import { cn } from "@/lib/utils";

export enum MobileSlot {
  Title = "title",
  Subtitle = "subtitle",
  Meta = "meta",
  Hidden = "hidden",
}

declare module "@tanstack/react-table" {
  interface ColumnMeta<TData extends RowData, TValue> {
    label?: string;
    sort?: string;
    filter?: FilterDef | FilterDef[];
    mobile?: MobileSlot;
    align?: "end";
    cellClass?: string;
  }
}

export interface DataTableSelection<T> {
  selected: ReadonlySet<string>;
  onChange: (next: Set<string>) => void;
  selectable?: (row: T) => boolean;
  limit?: number;
}

export interface TableList<T> {
  mode: ListMode;
  rows: T[];
  total?: number;
  status: "pending" | "error" | "success";
  error?: unknown;
  refetch: () => unknown;
  fetching?: boolean;
  hasMore?: boolean;
  loadingMore?: boolean;
  loadMore?: () => void;
}

export interface DataTableProps<T> {
  label: string;
  columns: ColumnDef<T, unknown>[];
  list: TableList<T>;
  state?: ListState;
  rowKey: (row: T) => string;
  rowHref?: (row: T) => string;
  empty: { icon: LucideIcon; title: string };
  selection?: DataTableSelection<T>;
  bulkActions?: ReactNode;
  rowActions?: (row: T) => ReactNode;
  rowHeight?: number;
}

export function columnSorts<T>(columns: ColumnDef<T, unknown>[]): Option[] {
  return columns.flatMap((column) => {
    const meta = column.meta;
    if (!meta?.sort) return [];
    return [{ value: meta.sort, label: meta.label ?? (typeof column.header === "string" ? column.header : meta.sort) }];
  });
}

export function columnFilters<T>(columns: ColumnDef<T, unknown>[]): FilterDef[] {
  return columns.flatMap((column) => metaFilters(column.meta?.filter));
}

function metaFilters(filter?: FilterDef | FilterDef[]): FilterDef[] {
  return filter ? [filter].flat() : [];
}

function headerTitle<T>(header: Header<T, unknown>): ReactNode {
  return header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext());
}

function ColumnHeader<T>({ header, state }: { header: Header<T, unknown>; state?: ListState }) {
  const meta = header.column.columnDef.meta;
  const title = headerTitle(header);
  const defs = metaFilters(meta?.filter);
  if (!state || (!meta?.sort && defs.length === 0)) return <>{title}</>;
  const sorted = meta?.sort && state.sort?.field === meta.sort ? state.sort : null;
  const filtered = defs.some((def) => !!state.values[def.field]);
  const wide = defs.some((def) => def.kind === FilterKind.Date || def.kind === FilterKind.Person);
  return (
    <Popover>
      <PopoverTrigger
        render={
          <button
            type="button"
            className={cn(
              "-mx-1.5 inline-flex h-7 items-center gap-1 rounded-md px-1.5 outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-signal/50 data-[popup-open]:bg-accent data-[popup-open]:text-foreground",
              (sorted || filtered) && "text-foreground",
            )}
          />
        }
      >
        {title}
        {sorted ? sorted.desc ? <ArrowDown aria-label="sorted descending" className="size-3.5" /> : <ArrowUp aria-label="sorted ascending" className="size-3.5" /> : null}
        {filtered ? <ListFilter aria-label="filtered" className="size-3.5 text-signal" /> : null}
        {!sorted && !filtered ? <ChevronsUpDown aria-hidden className="size-3.5 opacity-40" /> : null}
      </PopoverTrigger>
      <PopoverContent className={cn("flex max-h-[min(36rem,var(--available-height))] flex-col gap-3 overflow-y-auto", wide ? "w-80" : "w-72")}>
        {meta?.sort ? (
          <div className="flex flex-col gap-1.5">
            <span className="text-xs text-muted-foreground">Sort</span>
            <div className="grid grid-cols-2 gap-1.5">
              <PopoverClose render={<Button variant={sorted && !sorted.desc ? "secondary" : "outline"} size="sm" className="pointer-coarse:h-11" onClick={() => state.setSort({ field: meta.sort!, desc: false })} />}>
                <ArrowUp aria-hidden /> Ascending
              </PopoverClose>
              <PopoverClose render={<Button variant={sorted?.desc ? "secondary" : "outline"} size="sm" className="pointer-coarse:h-11" onClick={() => state.setSort({ field: meta.sort!, desc: true })} />}>
                <ArrowDown aria-hidden /> Descending
              </PopoverClose>
            </div>
            {sorted ? <PopoverClose render={<Button variant="ghost" size="sm" className="self-start pointer-coarse:h-11" onClick={() => state.setSort(null)} />}>Clear sort</PopoverClose> : null}
          </div>
        ) : null}
        {defs.map((def, index) => {
          const value = state.values[def.field] ?? "";
          return (
            <section key={def.field} aria-label={defs.length > 1 ? def.label : undefined} className="flex flex-col gap-2">
              {meta?.sort || index > 0 ? <div aria-hidden className="-mx-3 h-px bg-border" /> : null}
              {defs.length > 1 ? <span className="text-xs font-medium">{def.label}</span> : null}
              <FilterEditor def={def} value={value} autoFocus={index === 0} onChange={(next) => state.setFilter(def.field, next)} />
              {value ? <Button variant="ghost" size="sm" className="self-start pointer-coarse:h-11" onClick={() => state.setFilter(def.field, "")}><X aria-hidden /> Clear filter</Button> : null}
            </section>
          );
        })}
      </PopoverContent>
    </Popover>
  );
}

function Pagination({ page, size, total, onPage, onSize, fetching }: { page: number; size: number; total: number; onPage: (page: number) => void; onSize: (size: number) => void; fetching: boolean }) {
  const count = pageCount(total, size);
  const current = Math.min(page, count);
  const first = total === 0 ? 0 : (current - 1) * size + 1;
  const last = Math.min(total, current * size);
  return (
    <nav aria-label="Pages" className="flex h-12 shrink-0 items-center justify-between gap-3 border-t border-line bg-surface px-4 text-sm">
      <span className="tabular-nums text-muted-foreground" aria-live="polite" aria-busy={fetching}>
        {first.toLocaleString()}–{last.toLocaleString()} of {total.toLocaleString()}
      </span>
      <div className="flex items-center gap-3">
        <Select label="Rows" value={String(size)} options={pageSizes.map((n) => ({ value: String(n), label: String(n) }))} onChange={(value) => onSize(Number(value))} className="w-[4.5rem]" />
        <div className="flex items-center gap-0.5">
          <Button variant="ghost" size="icon-sm" aria-label="First page" disabled={current <= 1} onClick={() => onPage(1)}><ChevronsLeft /></Button>
          <Button variant="ghost" size="icon-sm" aria-label="Previous page" disabled={current <= 1} onClick={() => onPage(current - 1)}><ChevronLeft /></Button>
          {pageWindow(current, count).map((n, i) =>
            n === null ? (
              <span key={`gap-${i}`} aria-hidden className="w-6 text-center text-muted-foreground">…</span>
            ) : (
              <Button key={n} variant={n === current ? "secondary" : "ghost"} size="sm" aria-current={n === current ? "page" : undefined} aria-label={`Page ${n}`} className="min-w-7 px-1.5 tabular-nums" onClick={() => onPage(n)}>
                {n}
              </Button>
            ),
          )}
          <Button variant="ghost" size="icon-sm" aria-label="Next page" disabled={current >= count} onClick={() => onPage(current + 1)}><ChevronRight /></Button>
          <Button variant="ghost" size="icon-sm" aria-label="Last page" disabled={current >= count} onClick={() => onPage(count)}><ChevronsRight /></Button>
        </div>
      </div>
    </nav>
  );
}

function FloatingBar({ count, limit, onClear, children }: { count: number; limit: number; onClear: () => void; children: ReactNode }) {
  return (
    <div className="pointer-events-none absolute inset-x-0 bottom-16 z-20 flex justify-center px-3 max-md:bottom-4">
      <div role="region" aria-label="Bulk actions" className="pointer-events-auto flex max-w-full flex-wrap items-center gap-2 rounded-xl border border-border bg-popover px-3 py-2 text-sm shadow-overlay animate-rise motion-reduce:animate-none">
        <span className="font-medium tabular-nums">{count} selected</span>
        {count >= limit ? <span className="text-xs text-muted-foreground">limit {limit}</span> : null}
        <span aria-hidden className="h-5 w-px bg-border" />
        <span className="flex flex-wrap gap-1.5">{children}</span>
        <Button variant="ghost" size="icon-sm" aria-label="Clear selection" className="pointer-coarse:size-11" onClick={onClear}><X /></Button>
      </div>
    </div>
  );
}

function RowMenu({ children }: { children: ReactNode }) {
  if (children === null || children === undefined || children === false) return <span aria-hidden className="block size-8 pointer-coarse:size-11" />;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Row actions" className="pointer-coarse:size-11"><MoreHorizontal /></Button>} />
      <DropdownMenuContent align="end" className="w-48">{children}</DropdownMenuContent>
    </DropdownMenu>
  );
}

const interactive = "a,button,input,textarea,select,label,[role=menuitem],[role=checkbox]";

export function DataTable<T>({ label, columns, list, state, rowKey, rowHref, empty, selection, bulkActions, rowActions, rowHeight = 52 }: DataTableProps<T>) {
  const router = useRouter();
  const desktop = useIsDesktop();
  const viewport = useRef<HTMLDivElement>(null);
  const rows = list.rows;
  const allColumns = useMemo<ColumnDef<T, unknown>[]>(() => {
    const out: ColumnDef<T, unknown>[] = [];
    if (selection) {
      const limit = selection.limit ?? Infinity;
      const eligible = rows.filter((row) => selection.selectable?.(row) ?? true).map(rowKey);
      const picked = eligible.filter((key) => selection.selected.has(key)).length;
      const toggle = (key: string, on: boolean) => {
        const next = new Set(selection.selected);
        if (on && next.size < limit) next.add(key);
        else next.delete(key);
        selection.onChange(next);
      };
      out.push({
        id: "select",
        meta: { mobile: MobileSlot.Hidden, cellClass: "w-10" },
        header: () => (
          <Checkbox
            aria-label={`Select all ${label.toLowerCase()} on this page`}
            checked={eligible.length > 0 && picked === Math.min(eligible.length, limit)}
            indeterminate={picked > 0 && picked < Math.min(eligible.length, limit)}
            disabled={eligible.length === 0}
            onCheckedChange={(on) => selection.onChange(new Set(on ? eligible.slice(0, limit) : []))}
          />
        ),
        cell: ({ row }) => {
          const key = rowKey(row.original);
          const allowed = selection.selectable?.(row.original) ?? true;
          return (
            <Checkbox
              aria-label="Select row"
              checked={selection.selected.has(key)}
              disabled={!allowed || (!selection.selected.has(key) && selection.selected.size >= limit)}
              onCheckedChange={(on) => toggle(key, on)}
            />
          );
        },
      });
    }
    out.push(...columns);
    if (rowActions) {
      out.push({
        id: "actions",
        meta: { mobile: MobileSlot.Hidden, align: "end", cellClass: "w-12" },
        header: () => <span className="sr-only">Actions</span>,
        cell: ({ row }) => <RowMenu>{rowActions(row.original)}</RowMenu>,
      });
    }
    return out;
  }, [columns, selection, rows, rowKey, label, rowActions]);
  const table = useReactTable({ data: rows, columns: allColumns, getCoreRowModel: getCoreRowModel(), getRowId: rowKey, manualSorting: true, manualFiltering: true, manualPagination: true });
  const infinite = list.mode === ListMode.Infinite;
  const virtual = useVirtualizer({ count: rows.length, getScrollElement: () => viewport.current, estimateSize: () => (desktop ? rowHeight : 88), overscan: 8, enabled: infinite });
  const items = virtual.getVirtualItems();
  const lastIndex = items.at(-1)?.index ?? -1;
  const failed = list.status === "error";

  useEffect(() => {
    if (infinite && list.hasMore && !failed && !list.loadingMore && list.loadMore && lastIndex >= rows.length - 6) list.loadMore();
  }, [infinite, list, failed, lastIndex, rows.length]);

  const open = (event: MouseEvent<HTMLElement>, href?: string) => {
    if (!href || event.defaultPrevented) return;
    if ((event.target as HTMLElement).closest(interactive)) return;
    if (window.getSelection()?.toString()) return;
    router.push(href);
  };

  const tableRows = table.getRowModel().rows;
  const visible: { row: Row<T>; index: number; start?: number }[] = infinite
    ? items.map((item) => ({ row: tableRows[item.index], index: item.index, start: item.start }))
    : tableRows.map((row, index) => ({ row, index }));
  const top = infinite ? items[0]?.start ?? 0 : 0;
  const bottom = infinite ? virtual.getTotalSize() - (items.at(-1)?.end ?? 0) : 0;

  let content: ReactNode;
  if (failed && rows.length === 0) {
    content = <div className="p-4"><ErrorCard title={`Could not load ${label.toLowerCase()}`} error={list.error} onRetry={() => void list.refetch()} /></div>;
  } else if (list.status === "pending") {
    content = (
      <div role="status" aria-label={`Loading ${label.toLowerCase()}`} className="flex flex-col gap-2 p-4">
        {Array.from({ length: 8 }, (_, i) => <Skeleton key={i} className="h-10 w-full" />)}
      </div>
    );
  } else if (rows.length === 0) {
    content = state?.filtered ? (
      <EmptyState icon={SearchX} title="Nothing matches these filters">
        <Button variant="outline" size="sm" className="pointer-coarse:h-11" onClick={state.clear}>Clear filters</Button>
      </EmptyState>
    ) : (
      <EmptyState icon={empty.icon} title={empty.title} />
    );
  } else if (desktop) {
    const width = table.getVisibleLeafColumns().length;
    content = (
      <table aria-label={label} aria-rowcount={list.total !== undefined ? list.total + 1 : -1} className="w-full min-w-[44rem] border-collapse text-left text-sm">
        <thead className="sticky top-0 z-10 bg-surface text-xs text-muted-foreground shadow-[inset_0_-1px_0_var(--color-line)]">
          {table.getHeaderGroups().map((group) => (
            <tr key={group.id} aria-rowindex={1}>
              {group.headers.map((header) => {
                const meta = header.column.columnDef.meta;
                const sorted = meta?.sort && state?.sort?.field === meta.sort ? (state.sort.desc ? "descending" : "ascending") : meta?.sort ? "none" : undefined;
                return (
                  <th key={header.id} scope="col" aria-sort={sorted} className={cn("h-10 px-3 font-medium whitespace-nowrap first:pl-4 last:pr-4", meta?.align === "end" && "text-right", meta?.cellClass)}>
                    <ColumnHeader header={header} state={state} />
                  </th>
                );
              })}
            </tr>
          ))}
        </thead>
        <tbody className={cn("transition-opacity duration-150", list.fetching && "opacity-60")}>
          {top > 0 ? <tr aria-hidden><td colSpan={width} style={{ height: top }} /></tr> : null}
          {visible.map(({ row, index }) => {
            const href = rowHref?.(row.original);
            const picked = selection?.selected.has(row.id);
            return (
              <tr
                key={row.id}
                data-index={index}
                aria-rowindex={index + 2}
                aria-selected={selection ? !!picked : undefined}
                ref={infinite ? virtual.measureElement : undefined}
                onClick={(event) => open(event, href)}
                className={cn("border-b border-line align-middle", href && "cursor-pointer hover:bg-accent/60", picked && "bg-signal/6")}
                style={{ height: rowHeight }}
              >
                {row.getVisibleCells().map((cell) => {
                  const meta = cell.column.columnDef.meta;
                  return <td key={cell.id} className={cn("px-3 py-2 first:pl-4 last:pr-4", meta?.align === "end" && "text-right", meta?.cellClass)}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>;
                })}
              </tr>
            );
          })}
          {bottom > 0 ? <tr aria-hidden><td colSpan={width} style={{ height: bottom }} /></tr> : null}
        </tbody>
      </table>
    );
  } else {
    content = (
      <ul aria-label={label} className={cn("flex flex-col gap-2 p-3", list.fetching && "opacity-60")} style={infinite ? { paddingTop: top + 12, paddingBottom: bottom + 12 } : undefined}>
        {visible.map(({ row, index }) => {
          const href = rowHref?.(row.original);
          const cells = row.getVisibleCells();
          const slot = (cellSlot: MobileSlot) => cells.filter((cell) => (cell.column.columnDef.meta?.mobile ?? MobileSlot.Meta) === cellSlot);
          const select = cells.find((cell) => cell.column.id === "select");
          const picked = selection?.selected.has(row.id);
          return (
            <li
              key={row.id}
              data-index={index}
              ref={infinite ? virtual.measureElement : undefined}
              onClick={(event) => open(event, href)}
              className={cn("flex gap-3 rounded-xl border border-line bg-surface p-3", href && "cursor-pointer active:bg-accent/60", picked && "border-signal/40 bg-signal/6")}
            >
              {select ? <div className="flex h-6 items-center">{flexRender(select.column.columnDef.cell, select.getContext())}</div> : null}
              <div className="flex min-w-0 flex-1 flex-col gap-1.5">
                <div className="flex min-w-0 flex-col">
                  {slot(MobileSlot.Title).map((cell) => <div key={cell.id} className="min-w-0 font-medium">{flexRender(cell.column.columnDef.cell, cell.getContext())}</div>)}
                  {slot(MobileSlot.Subtitle).map((cell) => <div key={cell.id} className="min-w-0 text-sm text-muted-foreground">{flexRender(cell.column.columnDef.cell, cell.getContext())}</div>)}
                </div>
                {slot(MobileSlot.Meta).length ? (
                  <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
                    {slot(MobileSlot.Meta).map((cell) => (
                      <div key={cell.id} className="flex min-w-0 flex-col">
                        <dt className="text-muted-foreground">{cell.column.columnDef.meta?.label ?? (typeof cell.column.columnDef.header === "string" ? cell.column.columnDef.header : cell.column.id)}</dt>
                        <dd className="min-w-0 truncate">{flexRender(cell.column.columnDef.cell, cell.getContext())}</dd>
                      </div>
                    ))}
                  </dl>
                ) : null}
              </div>
              {rowActions ? <div className="-mt-1 -mr-1"><RowMenu>{rowActions(row.original)}</RowMenu></div> : null}
            </li>
          );
        })}
      </ul>
    );
  }

  const paged = list.mode === ListMode.Pages && state && list.total !== undefined && list.status !== "pending";
  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {list.fetching ? <div aria-hidden className="absolute inset-x-0 top-0 z-20 h-0.5 overflow-hidden bg-signal/15"><div className="h-full w-1/3 animate-progress bg-signal motion-reduce:animate-none" /></div> : null}
      <ScrollArea className="min-h-0 flex-1" orientation="both" viewportProps={{ ref: viewport, style: { scrollbarGutter: "stable" } }}>
        {content}
        {infinite && rows.length > 0 ? (
          <div className="flex h-14 items-center justify-center text-sm text-muted-foreground">
            {failed ? (
              <span role="alert" className="flex items-center gap-3">
                Could not load more
                <Button variant="outline" size="sm" className="pointer-coarse:h-11" onClick={list.hasMore ? list.loadMore : () => void list.refetch()} disabled={list.loadingMore}>Retry</Button>
              </span>
            ) : list.loadingMore ? <span aria-live="polite">Loading…</span> : list.hasMore ? null : <span>{rows.length.toLocaleString()} shown</span>}
          </div>
        ) : null}
      </ScrollArea>
      {paged ? <Pagination page={state.page} size={state.size} total={list.total!} onPage={state.setPage} onSize={state.setSize} fetching={!!list.fetching} /> : null}
      {selection && selection.selected.size > 0 && bulkActions ? (
        <FloatingBar count={selection.selected.size} limit={selection.limit ?? Infinity} onClear={() => selection.onChange(new Set())}>{bulkActions}</FloatingBar>
      ) : null}
    </div>
  );
}
