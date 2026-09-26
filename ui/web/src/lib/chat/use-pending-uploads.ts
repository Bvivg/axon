"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { UploadAs, UploadCancelled, uploadAttachment, type Uploaded } from "@/lib/chat/upload";

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

let nextID = 0;

export function usePendingUploads() {
  const [items, setItems] = useState<PendingUpload[]>([]);
  const controllers = useRef(new Map<string, AbortController>());
  const previews = useRef(new Map<string, string>());

  const patch = useCallback((id: string, change: Partial<PendingUpload>) => {
    setItems((current) => current.map((item) => (item.id === id ? { ...item, ...change } : item)));
  }, []);

  const forget = useCallback((id: string) => {
    controllers.current.get(id)?.abort();
    controllers.current.delete(id);
    const preview = previews.current.get(id);
    if (preview) {
      URL.revokeObjectURL(preview);
      previews.current.delete(id);
    }
  }, []);

  useEffect(() => {
    const pending = controllers.current;
    const urls = previews.current;
    return () => {
      pending.forEach((controller) => controller.abort());
      urls.forEach((url) => URL.revokeObjectURL(url));
    };
  }, []);

  const add = useCallback(
    (files: File[], as?: UploadAs) => {
      for (const file of files) {
        const mode = as ?? (isMedia(file) ? UploadAs.Media : UploadAs.File);
        const id = `upload-${(nextID += 1)}`;
        const shape = shapeOf(file, mode);
        const controller = new AbortController();
        controllers.current.set(id, controller);

        let previewUrl: string | undefined;
        if (shape === PendingShape.Image) {
          previewUrl = URL.createObjectURL(file);
          previews.current.set(id, previewUrl);
        }

        setItems((current) => [
          ...current,
          { id, name: file.name || "file", shape, previewUrl, progress: 0, status: PendingStatus.Uploading },
        ]);

        uploadAttachment(file, file.name || "file", mode, {
          signal: controller.signal,
          onProgress: (fraction) => patch(id, { progress: fraction }),
        })
          .then((uploaded) => {
            controllers.current.delete(id);
            patch(id, { status: PendingStatus.Ready, progress: 1, uploaded });
          })
          .catch((err: unknown) => {
            controllers.current.delete(id);
            if (err instanceof UploadCancelled) {
              return;
            }
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
      forget(id);
      setItems((current) => current.filter((item) => item.id !== id));
    },
    [forget],
  );

  const take = useCallback(
    (ids: string[]) => {
      ids.forEach(forget);
      setItems((current) => current.filter((item) => !ids.includes(item.id)));
    },
    [forget],
  );

  return {
    items,
    add,
    remove,
    take,
    busy: items.some((item) => item.status === PendingStatus.Uploading),
    ready: items.filter((item) => item.status === PendingStatus.Ready),
  };
}
