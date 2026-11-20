import type { QueryClient, QueryKey } from "@tanstack/react-query";

const coalesceMs = 600;

const pending = new Map<string, ReturnType<typeof setTimeout>>();

export function refreshSoon(queryClient: QueryClient, queryKey: QueryKey): void {
  const id = JSON.stringify(queryKey);
  if (pending.has(id)) return;
  pending.set(
    id,
    setTimeout(() => {
      pending.delete(id);
      void queryClient.invalidateQueries({ queryKey });
    }, coalesceMs),
  );
}
