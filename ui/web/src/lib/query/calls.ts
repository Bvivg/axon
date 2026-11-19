"use client";

import { useInfiniteQuery, type QueryClient } from "@tanstack/react-query";
import { callsClient } from "@/lib/connect/clients";

export const callHistoryKey = ["calls", "history"] as const;
const historyCoalesceMs = 600;

export function coalesceCallHistory(client: QueryClient) {
  const filters = { queryKey: callHistoryKey, exact: true };
  let timer: ReturnType<typeof setTimeout> | undefined;
  let dirty = false;
  let refreshing = false;
  let disposed = false;

  function changed() {
    if (disposed) return;
    dirty = true;
    if (refreshing) return;
    clearTimeout(timer);
    timer = setTimeout(() => { void refresh(); }, historyCoalesceMs);
  }

  async function refresh() {
    timer = undefined;
    refreshing = true;
    try {
      const query = client.getQueryCache().find(filters);
      if (query && query.state.fetchStatus !== "idle") {
        await query.promise?.catch(() => undefined);
      }
      if (disposed) return;
      dirty = false;
      await client.invalidateQueries(filters, { cancelRefetch: false });
    } finally {
      refreshing = false;
      if (dirty) changed();
    }
  }

  return {
    changed,
    dispose() {
      disposed = true;
      clearTimeout(timer);
    },
  };
}

export function useCallHistory() {
  return useInfiniteQuery({
    queryKey: callHistoryKey,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => callsClient.listCalls({ beforeId: pageParam, limit: 25 }, { signal }),
    getNextPageParam: page => page.nextBeforeId,
    retry: false,
  });
}
