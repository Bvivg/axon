"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useRef } from "react";

export type UrlState<K extends string> = Record<K, string>;

export function useUrlState<K extends string>(keys: readonly K[]): [UrlState<K>, (next: Partial<UrlState<K>>) => void, () => void] {
  const params = useSearchParams();
  const pathname = usePathname();
  const router = useRouter();
  const current = params.toString();
  const latest = useRef(current);
  useEffect(() => {
    latest.current = current;
  }, [current]);
  const state = Object.fromEntries(keys.map((key) => [key, params.get(key) ?? ""])) as UrlState<K>;
  const write = useCallback((merged: URLSearchParams) => {
    const query = merged.toString();
    latest.current = query;
    router.replace(query ? `${pathname}?${query}` : pathname, { scroll: false });
  }, [pathname, router]);
  const update = useCallback((next: Partial<UrlState<K>>) => {
    const merged = new URLSearchParams(latest.current);
    for (const [key, value] of Object.entries(next) as [K, string | undefined][]) {
      if (value) merged.set(key, value);
      else merged.delete(key);
    }
    write(merged);
  }, [write]);
  const clear = useCallback(() => {
    const merged = new URLSearchParams(latest.current);
    for (const key of keys) merged.delete(key);
    write(merged);
  }, [keys, write]);
  return [state, update, clear];
}
