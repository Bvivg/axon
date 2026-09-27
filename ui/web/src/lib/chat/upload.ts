import axios, { isAxiosError, isCancel } from "axios";

import { refreshSession } from "@/lib/auth/refresh";
import { getAccessToken } from "@/lib/auth/tokens";
import { apiBaseUrl } from "@/lib/connect/transport";

export enum UploadAs {
  Media = "media",
  File = "file",
  Voice = "voice",
}

export interface Uploaded {
  uploadId: string;
  kind: string;
  payload: Record<string, unknown>;
}

interface UploadResponse {
  upload_id: string;
  kind: string;
  payload: Record<string, unknown>;
}

export class UploadCancelled extends Error {
  constructor() {
    super("The upload was cancelled.");
  }
}

const limits: Record<UploadAs, string> = {
  [UploadAs.Media]: "Photos can be up to 25 MB and videos up to 100 MB.",
  [UploadAs.File]: "Files can be up to 100 MB.",
  [UploadAs.Voice]: "Voice messages can be up to 10 MB.",
};

async function accessToken(): Promise<string> {
  const token = getAccessToken();
  if (token) {
    return token;
  }
  if (!(await refreshSession())) {
    throw new Error("Your session has ended. Sign in again.");
  }
  return getAccessToken() ?? "";
}

export async function uploadAttachment(
  blob: Blob,
  filename: string,
  as: UploadAs,
  options: { onProgress?: (fraction: number) => void; signal?: AbortSignal } = {},
): Promise<Uploaded> {
  const attempt = async (token: string) =>
    axios.post<UploadResponse>(`${apiBaseUrl}/api/attachment`, blob, {
      params: { as },
      withCredentials: true,
      signal: options.signal,
      headers: {
        Authorization: `Bearer ${token}`,
        "Content-Type": blob.type || "application/octet-stream",
        "X-Filename": encodeURIComponent(filename),
      },
      onUploadProgress: (event) => {
        if (event.total) {
          options.onProgress?.(event.loaded / event.total);
        }
      },
    });

  try {
    let response;
    try {
      response = await attempt(await accessToken());
    } catch (err) {
      if (!isAxiosError(err) || err.response?.status !== 401 || !(await refreshSession())) {
        throw err;
      }
      response = await attempt(getAccessToken() ?? "");
    }

    return {
      uploadId: response.data.upload_id,
      kind: response.data.kind,
      payload: response.data.payload,
    };
  } catch (err) {
    throw explain(err, as);
  }
}

function explain(err: unknown, as: UploadAs): Error {
  if (isCancel(err)) {
    return new UploadCancelled();
  }
  if (!isAxiosError(err)) {
    return err instanceof Error ? err : new Error("The upload failed.");
  }

  switch (err.response?.status) {
    case undefined:
      return new Error("Could not reach the server. Check the connection and try again.");
    case 413:
      return new Error(`That file is too large. ${limits[as]}`);
    case 415:
      return new Error(
        as === UploadAs.Media
          ? "This file is not a photo or video we can open. Send it as a file instead."
          : "This recording could not be read.",
      );
    case 429:
      return new Error("Too many uploads at once. Wait a moment and try again.");
    case 504:
      return new Error("Processing took too long. Try a shorter video.");
    default:
      return new Error("The upload failed. Try again.");
  }
}
