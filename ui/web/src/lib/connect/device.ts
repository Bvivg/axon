import type { Interceptor } from "@connectrpc/connect";

const storageKey = "axon.device";
let cached: string | undefined;

export function deviceId() {
  if (cached) return cached;
  try {
    cached = localStorage.getItem(storageKey) ?? undefined;
    if (!cached) {
      cached = crypto.randomUUID();
      localStorage.setItem(storageKey, cached);
    }
  } catch {
    cached ??= crypto.randomUUID();
  }
  return cached;
}

export const deviceHeader: Interceptor = (next) => async (request) => {
  if (typeof window !== "undefined") request.header.set("X-Axon-Device", deviceId());
  return next(request);
};
