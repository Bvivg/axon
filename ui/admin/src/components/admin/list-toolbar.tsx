"use client";

import { SlidersHorizontal, X } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";

import { FilterEditor } from "@/components/admin/filter-editor";
import { SearchFilter } from "@/components/admin/filters";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select } from "@/components/ui/select";
import { Sheet } from "@/components/ui/sheet";
import type { ListState } from "@/lib/admin/list";
import { describeFilter, FilterKind, type Option } from "@/lib/admin/list-query";
import { useIsDesktop } from "@/lib/admin/media";
import { profileName } from "@/lib/format";
import { useAdminProfiles } from "@/lib/query/admin";

export function ListToolbar({
  state,
  search,
  sorts = [],
  children,
}: {
  state: ListState;
  search?: { label: string; key: string; placeholder?: string };
  sorts?: Option[];
  children?: ReactNode;
}) {
  const desktop = useIsDesktop();
  const [open, setOpen] = useState(false);
  const active = state.defs.filter((def) => state.values[def.field]);
  const people = useMemo(() => active.filter((def) => def.kind === FilterKind.Person).map((def) => state.values[def.field]), [active, state.values]);
  const profiles = useAdminProfiles(people);
  const name = (id: string) => profileName(profiles.data?.get(id));
  const sortValue = state.sort ? `${state.sort.field}:${state.sort.desc ? "desc" : "asc"}` : "";
  const sortOptions = [{ value: "", label: "Default order" }, ...sorts.flatMap((sort) => [
    { value: `${sort.value}:asc`, label: `${sort.label}, ascending` },
    { value: `${sort.value}:desc`, label: `${sort.label}, descending` },
  ])];
  const changeSort = (value: string) => {
    const [field, direction] = value.split(":");
    state.setSort(field ? { field, desc: direction === "desc" } : null);
  };
  const searchBox = search ? (
    <SearchFilter label={search.label} placeholder={search.placeholder} value={state.state[search.key] ?? ""} onChange={(value) => state.setExtra({ [search.key]: value })} className="md:max-w-xs" />
  ) : null;

  if (!desktop) {
    const count = active.length + (state.sort ? 1 : 0);
    return (
      <div role="search" className="flex h-[60px] shrink-0 items-center gap-2 border-b border-line bg-surface px-3">
        {searchBox ?? <span className="flex-1" />}
        <Button variant={count ? "secondary" : "outline"} className="h-11 shrink-0" onClick={() => setOpen(true)}>
          <SlidersHorizontal aria-hidden /> Filters{count ? ` · ${count}` : ""}
        </Button>
        <Sheet
          open={open}
          onOpenChange={setOpen}
          title="Filters"
          footer={
            <>
              <Button variant="ghost" className="h-11" onClick={() => { state.clear(); setOpen(false); }}>Clear all</Button>
              <Button className="h-11" onClick={() => setOpen(false)}>Show results</Button>
            </>
          }
        >
          <div className="flex flex-col gap-6">
            {children ? <div className="flex flex-col gap-3">{children}</div> : null}
            {sorts.length ? (
              <Select label="Sort by" value={sortValue} options={sortOptions} onChange={changeSort} className="flex-1" />
            ) : null}
            {state.defs.map((def) => (
              <section key={def.field} aria-label={def.label} className="flex flex-col gap-2">
                <div className="flex items-center justify-between">
                  <h3 className="text-sm font-medium">{def.label}</h3>
                  {state.values[def.field] ? <Button variant="ghost" size="sm" className="h-11" onClick={() => state.setFilter(def.field, "")}>Clear</Button> : null}
                </div>
                <FilterEditor def={def} value={state.values[def.field] ?? ""} onChange={(value) => state.setFilter(def.field, value)} />
              </section>
            ))}
          </div>
        </Sheet>
      </div>
    );
  }

  return (
    <div role="search" className="flex h-[52px] shrink-0 items-center gap-2 border-b border-line bg-surface px-4">
      {searchBox}
      {children}
      {sorts.length ? <Select label="Sort" value={sortValue} options={sortOptions} onChange={changeSort} className="w-52" /> : null}
      <ScrollArea orientation="horizontal" className="min-w-0 flex-1">
        <ul aria-label="Active filters" className="flex h-[52px] items-center gap-1.5 whitespace-nowrap">
          {active.map((def) => (
            <li key={def.field}>
              <span className="inline-flex h-7 items-center gap-1 rounded-full border border-signal/30 bg-signal/10 pr-0.5 pl-2.5 text-xs">
                <span className="text-muted-foreground">{def.label}:</span>
                <span className="max-w-[14rem] truncate font-medium">{describeFilter(def, state.values[def.field], name)}</span>
                <button type="button" aria-label={`Remove ${def.label} filter`} className="grid size-6 place-items-center rounded-full text-muted-foreground hover:bg-signal/15 hover:text-foreground" onClick={() => state.setFilter(def.field, "")}>
                  <X className="size-3.5" />
                </button>
              </span>
            </li>
          ))}
        </ul>
      </ScrollArea>
      {state.filtered ? <Button variant="ghost" size="sm" className="shrink-0" onClick={state.clear}>Clear all</Button> : null}
    </div>
  );
}
