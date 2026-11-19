import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";

import { callHistoryKey, coalesceCallHistory } from "./calls";

afterEach(() => vi.useRealTimers());

it("refreshes the active history once after a burst of call updates", async () => {
  vi.useFakeTimers();
  const client = new QueryClient();
  client.setQueryData(callHistoryKey, "old history");
  const queryFn = vi.fn(async () => "completed call");
  const observer = new QueryObserver(client, { queryKey: callHistoryKey, queryFn, staleTime: Infinity });
  const unsubscribe = observer.subscribe(() => {});
  const history = coalesceCallHistory(client);

  for (let version = 0; version < 100; version++) {
    history.changed();
    await vi.advanceTimersByTimeAsync(10);
  }
  expect(queryFn).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(600);
  expect(queryFn).toHaveBeenCalledTimes(1);
  expect(observer.getCurrentResult().data).toBe("completed call");

  history.dispose();
  unsubscribe();
  client.clear();
});

it("waits for an existing history read and refreshes again for changes during its own read", async () => {
  vi.useFakeTimers();
  const client = new QueryClient();
  client.setQueryData(callHistoryKey, "old history");
  const pending: { signal: AbortSignal; resolve: (data: string) => void }[] = [];
  const observer = new QueryObserver(client, {
    queryKey: callHistoryKey,
    staleTime: Infinity,
    queryFn: ({ signal }) => new Promise<string>(resolve => pending.push({ signal, resolve })),
  });
  const unsubscribe = observer.subscribe(() => {});
  const history = coalesceCallHistory(client);
  const initial = observer.refetch();

  history.changed();
  await vi.advanceTimersByTimeAsync(600);
  expect(pending).toHaveLength(1);
  expect(pending[0].signal.aborted).toBe(false);
  pending[0].resolve("initial read");
  await initial;
  await vi.advanceTimersByTimeAsync(0);
  expect(pending).toHaveLength(2);

  history.changed();
  history.changed();
  await vi.advanceTimersByTimeAsync(600);
  expect(pending).toHaveLength(2);
  expect(pending[1].signal.aborted).toBe(false);
  pending[1].resolve("call in progress");
  await vi.advanceTimersByTimeAsync(600);
  expect(pending).toHaveLength(3);
  pending[2].resolve("completed call");
  await vi.advanceTimersByTimeAsync(0);
  expect(observer.getCurrentResult().data).toBe("completed call");

  history.changed();
  history.dispose();
  await vi.advanceTimersByTimeAsync(600);
  expect(pending).toHaveLength(3);
  unsubscribe();
  client.clear();
});
