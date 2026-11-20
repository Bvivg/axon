import { hashBlob } from "@/lib/chat/hash-blob";

export type HashReply = { progress: number } | { hash: string } | { error: string };

self.onmessage = async (event: MessageEvent<Blob>) => {
  try {
    const hash = await hashBlob(event.data, (progress) => self.postMessage({ progress }));
    self.postMessage({ hash });
  } catch (error) {
    self.postMessage({ error: error instanceof Error ? error.message : String(error) });
  }
};
