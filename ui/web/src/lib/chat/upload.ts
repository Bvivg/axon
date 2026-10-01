import axios, { isAxiosError, isCancel, type AxiosRequestConfig } from "axios";

import { refreshSession } from "@/lib/auth/refresh";
import { getAccessToken } from "@/lib/auth/tokens";
import { apiBaseUrl } from "@/lib/connect/transport";

export enum UploadAs {
  Media = "media",
  File = "file",
  Voice = "voice",
}

export enum UploadStage {
  Uploading = "uploading",
  Processing = "processing",
}

export interface Uploaded {
  uploadId: string;
  kind: string;
  payload: Record<string, unknown>;
}

interface SessionResponse {
  id: string;
  status: "uploading" | "processing" | "ready" | "failed";
  part_size: number;
  part_count: number;
  parts: number[];
  failure?: string;
  upload?: { upload_id: string; kind: string; payload: Record<string, unknown> };
}

export class UploadCancelled extends Error {
  constructor() {
    super("The upload was cancelled.");
  }
}

const sessionsUrl = `${apiBaseUrl}/api/uploads`;
const partAttempts = 4;
const retryDelayMs = 1_000;

const limits: Record<UploadAs, string> = {
  [UploadAs.Media]: "Photos can be up to 25 MB and videos up to 200 MB.",
  [UploadAs.File]: "Files can be up to 200 MB.",
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

const throttled = (err: unknown) => isAxiosError(err) && err.response?.status === 429;

async function call<T>(config: AxiosRequestConfig): Promise<T> {
  for (let attempt = 1; ; attempt += 1) {
    try {
      return await callOnce<T>(config);
    } catch (err) {
      if (attempt >= partAttempts || !throttled(err)) throw err;
      await pause(retryDelayMs * 2 ** (attempt - 1), config.signal as AbortSignal | undefined);
    }
  }
}

async function callOnce<T>(config: AxiosRequestConfig): Promise<T> {
  const attempt = async (token: string) =>
    axios.request<T>({
      ...config,
      withCredentials: true,
      headers: { ...config.headers, Authorization: `Bearer ${token}` },
    });

  try {
    return (await attempt(await accessToken())).data;
  } catch (err) {
    if (!isAxiosError(err) || err.response?.status !== 401 || !(await refreshSession())) {
      throw err;
    }
    return (await attempt(getAccessToken() ?? "")).data;
  }
}

const retriable = (err: unknown) =>
  isAxiosError(err) && !isCancel(err) && (err.response === undefined || err.response.status === 429 || err.response.status >= 500);

const pause = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        reject(new UploadCancelled());
      },
      { once: true },
    );
  });

export async function uploadAttachment(
  blob: Blob,
  filename: string,
  as: UploadAs,
  options: {
    onProgress?: (fraction: number) => void;
    onStage?: (stage: UploadStage) => void;
    onSession?: (sessionID: string) => void;
    signal?: AbortSignal;
  } = {},
): Promise<Uploaded> {
  const { signal } = options;
  try {
    const session = await call<SessionResponse>({
      method: "POST",
      url: sessionsUrl,
      data: { as, name: filename, size: blob.size },
      signal,
    });
    options.onSession?.(session.id);

    let sent = 0;
    for (let number = 1; number <= session.part_count; number += 1) {
      const start = (number - 1) * session.part_size;
      const part = blob.slice(start, Math.min(blob.size, start + session.part_size));
      await sendPart(session.id, number, part, signal, (loaded) =>
        options.onProgress?.(Math.min(1, (sent + loaded) / blob.size)),
      );
      sent += part.size;
    }
    options.onProgress?.(1);

    options.onStage?.(UploadStage.Processing);
    let state = await call<SessionResponse>({ method: "POST", url: `${sessionsUrl}/${session.id}/complete`, signal });
    while (state.status === "processing") {
      state = await call<SessionResponse>({ method: "GET", url: `${sessionsUrl}/${session.id}`, params: { wait: 1 }, signal });
    }

    if (state.status === "ready" && state.upload) {
      return { uploadId: state.upload.upload_id, kind: state.upload.kind, payload: state.upload.payload };
    }
    throw failed(state.failure, as);
  } catch (err) {
    throw explain(err, as);
  }
}

async function sendPart(
  sessionID: string,
  number: number,
  part: Blob,
  signal: AbortSignal | undefined,
  onLoaded: (loaded: number) => void,
) {
  for (let attempt = 1; ; attempt += 1) {
    try {
      await call<void>({
        method: "PUT",
        url: `${sessionsUrl}/${sessionID}/parts/${number}`,
        data: part,
        headers: { "Content-Type": "application/octet-stream" },
        signal,
        onUploadProgress: (event) => onLoaded(event.loaded),
      });
      return;
    } catch (err) {
      if (attempt >= partAttempts || !retriable(err)) throw err;
      onLoaded(0);
      await pause(retryDelayMs * 2 ** (attempt - 1), signal);
    }
  }
}

export async function withdrawUpload(sessionID: string, options: { keepalive?: boolean } = {}): Promise<void> {
  const token = getAccessToken();
  if (!token) return;
  try {
    await fetch(`${sessionsUrl}/${sessionID}`, {
      method: "DELETE",
      headers: { Authorization: `Bearer ${token}` },
      credentials: "include",
      keepalive: options.keepalive,
    });
  } catch {
    return;
  }
}

function failed(reason: string | undefined, as: UploadAs): Error {
  switch (reason) {
    case "too_large":
      return new Error(`That file is too large. ${limits[as]}`);
    case "unsupported":
      return new Error(
        as === UploadAs.Media
          ? "This file is not a photo or video we can open. Send it as a file instead."
          : "This recording could not be read.",
      );
    case "timeout":
      return new Error("Processing took too long. Try a shorter video.");
    default:
      return new Error("The upload failed. Try again.");
  }
}

function explain(err: unknown, as: UploadAs): Error {
  if (isCancel(err) || err instanceof UploadCancelled) {
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
    case 429:
      return new Error("Too many uploads at once. Wait a moment and try again.");
    default:
      return new Error("The upload failed. Try again.");
  }
}
