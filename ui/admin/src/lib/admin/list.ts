"use client";

import { Code } from "@connectrpc/connect";
import { keepPreviousData, useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

import { useIsDesktop } from "@/lib/admin/media";
import {
  defaultPageSize,
  filterPrefix,
  formatSort,
  pageNumber,
  pageSize,
  parseSort,
  toFilter,
  toOrderBy,
  type FilterDef,
  type SortState,
} from "@/lib/admin/list-query";
import { useUrlState } from "@/lib/admin/url-state";
import { isCode } from "@/lib/errors";

export interface ListRequest {
  offset: number;
  pageSize: number;
  filter: string;
  orderBy: string;
}

export interface ListPage<T> {
  items: T[];
  total: number;
}

export function useListState(defs: readonly FilterDef[], extra: readonly string[] = []) {
  const keys = useMemo(() => ["page", "size", "sort", ...extra, ...defs.map((def) => filterPrefix + def.field)], [defs, extra]);
  const [state, update, clear] = useUrlState(keys);
  const values = useMemo(() => Object.fromEntries(defs.map((def) => [def.field, state[filterPrefix + def.field] ?? ""])), [defs, state]);
  const sort = parseSort(state.sort ?? "");
  const filter = toFilter(defs, values);
  const setFilter = useCallback((field: string, value: string) => update({ [filterPrefix + field]: value, page: "" }), [update]);
  const setSort = useCallback((next: SortState | null) => update({ sort: formatSort(next), page: "" }), [update]);
  const setPage = useCallback((page: number) => update({ page: page > 1 ? String(page) : "" }), [update]);
  const setSize = useCallback((size: number) => update({ size: size === defaultPageSize ? "" : String(size), page: "" }), [update]);
  const setExtra = useCallback((patch: Record<string, string>) => update({ ...patch, page: "" }), [update]);
  const extraActive = extra.some((key) => !!state[key]);
  return {
    state,
    values,
    defs,
    sort,
    filter,
    orderBy: toOrderBy(sort),
    page: pageNumber(state.page ?? ""),
    size: pageSize(state.size ?? ""),
    filtered: !!filter || extraActive,
    setFilter,
    setSort,
    setPage,
    setSize,
    setExtra,
    clear,
  };
}

export type ListState = ReturnType<typeof useListState>;

const retry = (failures: number, error: unknown) => !isCode(error, Code.Unimplemented) && failures < 3;

export enum ListMode {
  Pages = "pages",
  Infinite = "infinite",
}

export function useAdminList<T>(
  key: readonly unknown[],
  list: ListState,
  load: (request: ListRequest, signal: AbortSignal) => Promise<ListPage<T>>,
  rowKey: (row: T) => string,
  enabled = true,
  refetchInterval?: number,
) {
  const desktop = useIsDesktop();
  const mode = desktop ? ListMode.Pages : ListMode.Infinite;
  const base = [...key, list.filter, list.orderBy] as const;
  const paged = useQuery({
    queryKey: [...base, "page", list.page, list.size],
    queryFn: ({ signal }) => load({ offset: (list.page - 1) * list.size, pageSize: list.size, filter: list.filter, orderBy: list.orderBy }, signal),
    placeholderData: keepPreviousData,
    enabled: enabled && desktop,
    retry,
    refetchInterval,
  });
  const infinite = useInfiniteQuery({
    queryKey: [...base, "infinite"],
    queryFn: ({ pageParam, signal }) => load({ offset: pageParam, pageSize: defaultPageSize, filter: list.filter, orderBy: list.orderBy }, signal),
    initialPageParam: 0,
    getNextPageParam: (last: ListPage<T>, pages: ListPage<T>[]) => {
      const loaded = pages.reduce((sum, page) => sum + page.items.length, 0);
      return loaded < last.total && last.items.length > 0 ? loaded : undefined;
    },
    enabled: enabled && !desktop,
    retry,
    refetchInterval,
  });
  const rows = useMemo(() => {
    if (desktop) return paged.data?.items ?? [];
    const seen = new Set<string>();
    const out: T[] = [];
    for (const page of infinite.data?.pages ?? []) {
      for (const item of page.items) {
        const id = rowKey(item);
        if (seen.has(id)) continue;
        seen.add(id);
        out.push(item);
      }
    }
    return out;
  }, [desktop, paged.data, infinite.data, rowKey]);
  const total = desktop ? paged.data?.total : infinite.data?.pages[0]?.total;
  const query = desktop ? paged : infinite;
  return {
    mode,
    rows,
    total,
    status: query.status,
    error: query.error,
    refetch: query.refetch,
    fetching: desktop ? paged.isFetching && paged.isPlaceholderData : false,
    hasMore: !desktop && infinite.hasNextPage,
    loadingMore: !desktop && infinite.isFetchingNextPage,
    loadMore: () => void infinite.fetchNextPage(),
  };
}

export type AdminList<T> = ReturnType<typeof useAdminList<T>>;
