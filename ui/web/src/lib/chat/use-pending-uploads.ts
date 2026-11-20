"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { prepareImage } from "@/lib/chat/prepare-image";
import { UploadAs, UploadCancelled, UploadStage, uploadAttachment, withdrawUpload, type Uploaded } from "@/lib/chat/upload";

export enum PendingStatus {
  Uploading = "uploading",
  Ready = "ready",
  Failed = "failed",
}

export enum PendingShape {
  Image = "image",
  Video = "video",
  File = "file",
}

export interface PendingUpload {
  id: string;
  name: string;
  shape: PendingShape;
  previewUrl?: string;
  progress: number;
  processing?: boolean;
  hashing?: boolean;
  status: PendingStatus;
  uploaded?: Uploaded;
  error?: string;
}

function shapeOf(file: File, as: UploadAs): PendingShape {
  if (as === UploadAs.Media && file.type.startsWith("image/")) return PendingShape.Image;
  if (as === UploadAs.Media && file.type.startsWith("video/")) return PendingShape.Video;
  return PendingShape.File;
}

function isMedia(file: File): boolean {
  return file.type.startsWith("image/") || file.type.startsWith("video/");
}

const maxAttachments = 10;
let nextID = 0;

export function usePendingUploads() {
  const [items, setItems] = useState<PendingUpload[]>([]);
  const [selectionError, setSelectionError] = useState<string | null>(null);
  const activeIds = useRef(new Set<string>());
  const controllers = useRef(new Map<string, AbortController>());
  const previews = useRef(new Map<string, string>());
  const sessions = useRef(new Map<string, string>());

  const patch = useCallback((id: string, change: Partial<PendingUpload>) => {
    setItems((current) => current.map((item) => (item.id === id ? { ...item, ...change } : item)));
  }, []);

  const forget = useCallback((id: string, withdraw: boolean) => {
    activeIds.current.delete(id);
    controllers.current.get(id)?.abort();
    controllers.current.delete(id);
    const session = sessions.current.get(id);
    sessions.current.delete(id);
    if (session && withdraw) {
      void withdrawUpload(session);
    }
    const preview = previews.current.get(id);
    if (preview) {
      URL.revokeObjectURL(preview);
      previews.current.delete(id);
    }
  }, []);

  useEffect(() => {
    const pending = controllers.current;
    const urls = previews.current;
    const drafts = sessions.current;
    const onPageHide = () => drafts.forEach((session) => void withdrawUpload(session, { keepalive: true }));
    window.addEventListener("pagehide", onPageHide);
    return () => {
      window.removeEventListener("pagehide", onPageHide);
      pending.forEach((controller) => controller.abort());
      urls.forEach((url) => URL.revokeObjectURL(url));
      drafts.forEach((session) => void withdrawUpload(session));
      drafts.clear();
    };
  }, []);

  const add = useCallback(
    (files: File[], as?: UploadAs) => {
      if (activeIds.current.size + files.length > maxAttachments) {
        setSelectionError(`A message can contain up to ${maxAttachments} attachments.`);
        return;
      }
      setSelectionError(null);
      for (const file of files) {
        const mode = as ?? (isMedia(file) ? UploadAs.Media : UploadAs.File);
        const id = `upload-${(nextID += 1)}`;
        const shape = shapeOf(file, mode);
        const controller = new AbortController();
        activeIds.current.add(id);
        controllers.current.set(id, controller);

        let previewUrl: string | undefined;
        if (shape !== PendingShape.File) {
          previewUrl = URL.createObjectURL(file);
          previews.current.set(id, previewUrl);
        }

        setItems((current) => [
          ...current,
          { id, name: file.name || "file", shape, previewUrl, progress: 0, status: PendingStatus.Uploading },
        ]);

        const prepared = shape === PendingShape.Image ? prepareImage(file) : Promise.resolve(file);
        prepared
          .then((ready) =>
            uploadAttachment(ready, ready.name || "file", mode, {
              signal: controller.signal,
              onProgress: (fraction) => patch(id, { progress: fraction }),
              onStage: (stage) => patch(id, { processing: stage === UploadStage.Processing, hashing: stage === UploadStage.Hashing }),
              onSession: (session) => {
                if (controllers.current.has(id)) sessions.current.set(id, session);
                else void withdrawUpload(session);
              },
            }),
          )
          .then((uploaded) => {
            controllers.current.delete(id);
            patch(id, { status: PendingStatus.Ready, progress: 1, processing: false, hashing: false, uploaded });
          })
          .catch((err: unknown) => {
            controllers.current.delete(id);
            if (err instanceof UploadCancelled) {
              return;
            }
            const session = sessions.current.get(id);
            sessions.current.delete(id);
            if (session) void withdrawUpload(session);
            patch(id, {
              status: PendingStatus.Failed,
              error: err instanceof Error ? err.message : "The upload failed.",
            });
          });
      }
    },
    [patch],
  );

  const remove = useCallback(
    (id: string) => {
      forget(id, true);
      setSelectionError(null);
      setItems((current) => current.filter((item) => item.id !== id));
    },
    [forget],
  );

  const take = useCallback(
    (ids: string[]) => {
      ids.forEach((id) => forget(id, false));
      setSelectionError(null);
      setItems((current) => current.filter((item) => !ids.includes(item.id)));
    },
    [forget],
  );

  const clear = useCallback(() => {
    activeIds.current.forEach((id) => forget(id, true));
    setSelectionError(null);
    setItems([]);
  }, [forget]);

  return {
    items,
    add,
    remove,
    take,
    clear,
    busy: items.some((item) => item.status === PendingStatus.Uploading),
    selectionError,
  };
}
