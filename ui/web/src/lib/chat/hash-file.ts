import type { HashReply } from "@/lib/chat/hash.worker";

export function hashFile(blob: Blob, signal: AbortSignal, onProgress?: (fraction: number) => void): Promise<string> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(new DOMException("Aborted", "AbortError"));
      return;
    }
    const worker = new Worker(new URL("./hash.worker.ts", import.meta.url), { type: "module" });
    const cleanup = () => {
      signal.removeEventListener("abort", abort);
      worker.terminate();
    };
    const fail = (error: Error) => { cleanup(); reject(error); };
    const abort = () => fail(new DOMException("Aborted", "AbortError"));
    worker.onmessage = ({ data }: MessageEvent<HashReply>) => {
      if ("progress" in data) onProgress?.(data.progress);
      else if ("error" in data) fail(new Error(data.error));
      else { cleanup(); resolve(data.hash); }
    };
    worker.onerror = (event) => fail(new Error(event.message));
    worker.onmessageerror = () => fail(new Error("Could not read the file checksum."));
    signal.addEventListener("abort", abort, { once: true });
    try { worker.postMessage(blob); } catch (error) {
      fail(error instanceof Error ? error : new Error(String(error)));
    }
  });
}
