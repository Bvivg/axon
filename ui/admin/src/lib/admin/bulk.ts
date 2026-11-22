import { Code, ConnectError } from "@connectrpc/connect";

import { retryAfterSeconds } from "@/lib/errors";

export const bulkLimit = 100;
export const bulkConcurrency = 4;
export const bulkAttempts = 3;

export interface BulkFailure<T> {
  item: T;
  error: unknown;
}

export interface BulkResult<T> {
  done: T[];
  failed: BulkFailure<T>[];
}

export interface BulkOptions {
  concurrency?: number;
  attempts?: number;
  wait?: (seconds: number) => Promise<void>;
}

function pause(seconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, seconds * 1000));
}

export function throttledFor(error: unknown): number | undefined {
  if (!(error instanceof ConnectError) || error.code !== Code.ResourceExhausted) return undefined;
  return retryAfterSeconds(error) ?? 1;
}

export async function runBulk<T>(items: readonly T[], run: (item: T) => Promise<unknown>, options: BulkOptions = {}): Promise<BulkResult<T>> {
  const { concurrency = bulkConcurrency, attempts = bulkAttempts, wait = pause } = options;
  const done: T[] = [];
  const failed: BulkFailure<T>[] = [];
  let next = 0;
  const attempt = async (item: T, left: number): Promise<void> => {
    try {
      await run(item);
      done.push(item);
    } catch (error) {
      const seconds = throttledFor(error);
      if (seconds === undefined || left <= 1) {
        failed.push({ item, error });
        return;
      }
      await wait(seconds);
      await attempt(item, left - 1);
    }
  };
  const worker = async () => {
    while (next < items.length) {
      await attempt(items[next++], attempts);
    }
  };
  await Promise.all(Array.from({ length: Math.max(1, Math.min(concurrency, items.length)) }, worker));
  return { done, failed };
}

export function groupFailures<T>(failed: readonly BulkFailure<T>[], describe: (error: unknown) => string): [string, number][] {
  const groups = new Map<string, number>();
  for (const { error } of failed) {
    const message = describe(error);
    groups.set(message, (groups.get(message) ?? 0) + 1);
  }
  return [...groups.entries()].sort((a, b) => b[1] - a[1]);
}
