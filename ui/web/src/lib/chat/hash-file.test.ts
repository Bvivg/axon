import { afterEach, describe, expect, it, vi } from "vitest";

import { hashFile } from "@/lib/chat/hash-file";

class FakeWorker {
  static last: FakeWorker;
  onmessage?: (event: MessageEvent) => void;
  onerror?: (event: ErrorEvent) => void;
  terminate = vi.fn();
  postMessage = vi.fn();
  constructor() { FakeWorker.last = this; }
}

afterEach(() => vi.unstubAllGlobals());

describe("hash worker lifecycle", () => {
  it("terminates the worker when the attachment is removed", async () => {
    vi.stubGlobal("Worker", FakeWorker);
    const controller = new AbortController();
    const result = hashFile(new Blob(["bytes"]), controller.signal);
    const assertion = expect(result).rejects.toMatchObject({ name: "AbortError" });
    controller.abort();
    await assertion;
    expect(FakeWorker.last.terminate).toHaveBeenCalledOnce();
  });

  it("reports progress and releases the worker after a hash", async () => {
    vi.stubGlobal("Worker", FakeWorker);
    const progress = vi.fn();
    const result = hashFile(new Blob(["bytes"]), new AbortController().signal, progress);
    FakeWorker.last.onmessage?.({ data: { progress: 0.5 } } as MessageEvent);
    FakeWorker.last.onmessage?.({ data: { hash: "checksum" } } as MessageEvent);
    await expect(result).resolves.toBe("checksum");
    expect(progress).toHaveBeenCalledWith(0.5);
    expect(FakeWorker.last.terminate).toHaveBeenCalledOnce();
  });

  it("does not create a worker for an already cancelled attachment", async () => {
    const worker = vi.fn();
    vi.stubGlobal("Worker", worker);
    const controller = new AbortController();
    controller.abort();
    await expect(hashFile(new Blob(["bytes"]), controller.signal)).rejects.toMatchObject({ name: "AbortError" });
    expect(worker).not.toHaveBeenCalled();
  });
});
