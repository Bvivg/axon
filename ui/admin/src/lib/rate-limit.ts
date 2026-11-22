export const rateLimitRetries = 2;

export function retryAfterMs(value: string | null | undefined, now = Date.now()): number {
  if (!value?.trim()) return 1_000;
  const seconds = Number(value);
  if (Number.isFinite(seconds)) return seconds >= 0 ? Math.max(1, Math.ceil(seconds * 1_000)) : 1_000;
  const date = Date.parse(value);
  return Number.isFinite(date) ? Math.max(1, date - now) : 1_000;
}

export function waitForRetry(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
      return;
    }
    const abort = () => {
      clearTimeout(timer);
      reject(signal?.reason ?? new DOMException("Aborted", "AbortError"));
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", abort, { once: true });
  });
}

export async function fetchWithRateLimitRetry(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const request = new Request(input, init);
  for (let retry = 0; ; retry += 1) {
    request.signal.throwIfAborted();
    const response = await fetch(request.clone());
    if (response.status !== 429 || retry >= rateLimitRetries) return response;
    const delay = retryAfterMs(response.headers.get("Retry-After"));
    await response.body?.cancel();
    await waitForRetry(delay, request.signal);
  }
}
