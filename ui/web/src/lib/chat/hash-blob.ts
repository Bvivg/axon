import { sha256 } from "@noble/hashes/sha2.js";
import { bytesToHex } from "@noble/hashes/utils.js";

const chunkBytes = 2 * 1024 * 1024;

export async function hashBlob(blob: Blob, onProgress: (fraction: number) => void): Promise<string> {
  const hash = sha256.create();
  try {
    for (let offset = 0; offset < blob.size; offset += chunkBytes) {
      hash.update(new Uint8Array(await blob.slice(offset, offset + chunkBytes).arrayBuffer()));
      onProgress(Math.min(1, (offset + chunkBytes) / blob.size));
    }
    return bytesToHex(hash.digest());
  } finally {
    hash.destroy();
  }
}
