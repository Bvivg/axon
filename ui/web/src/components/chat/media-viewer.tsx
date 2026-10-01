"use client";

import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import { Download, X } from "lucide-react";
import Image from "next/image";
import type { MouseEvent } from "react";

import { ContentKind, contentOf } from "@/lib/chat/content";
import type { WireMessage } from "@/lib/ws/protocol";

export function MediaViewer({ message, onClose }: { message: WireMessage | null; onClose: () => void }) {
  const content = message ? contentOf(message) : null;
  const caption = message?.body.trim() ?? "";

  const isImage = content?.kind === ContentKind.Image;
  const isVideo = content?.kind === ContentKind.Video;
  const original = isImage ? content.image.url : isVideo ? content.video.url : "";

  const closeOnEmpty = (event: MouseEvent<HTMLElement>) => {
    if (event.target === event.currentTarget) onClose();
  };

  return (
    <BaseDialog.Root open={isImage || isVideo} onOpenChange={(open) => (open ? undefined : onClose())}>
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className="fixed inset-0 z-50 bg-black/90 transition-opacity duration-200 ease-signal data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <BaseDialog.Popup
          onClick={closeOnEmpty}
          className="fixed inset-0 z-50 flex flex-col outline-none transition-opacity duration-200 ease-signal data-[ending-style]:opacity-0 data-[starting-style]:opacity-0"
        >
          <BaseDialog.Title className="sr-only">{isVideo ? "Video" : "Photo"}</BaseDialog.Title>
          <div className="flex shrink-0 items-center justify-end gap-1 p-3" onClick={closeOnEmpty}>
            <a
              href={original}
              target="_blank"
              rel="noopener noreferrer"
              aria-label="Open original"
              className="flex size-9 items-center justify-center rounded-full text-white/80 transition-colors hover:bg-white/10 hover:text-white"
            >
              <Download aria-hidden className="size-5" />
            </a>
            <BaseDialog.Close
              aria-label="Close"
              className="flex size-9 items-center justify-center rounded-full text-white/80 transition-colors hover:bg-white/10 hover:text-white"
            >
              <X aria-hidden className="size-5" />
            </BaseDialog.Close>
          </div>

          <div className="flex min-h-0 flex-1 items-center justify-center px-4 pb-4" onClick={closeOnEmpty}>
            {isImage ? (
              <Image
                src={content.image.url}
                alt={caption || "Photo"}
                width={content.image.width || 1600}
                height={content.image.height || 1200}
                unoptimized
                sizes="100vw"
                className="h-auto max-h-full w-auto max-w-full object-contain"
              />
            ) : null}
            {isVideo ? (
              <video
                src={content.video.url}
                poster={content.video.posterUrl || undefined}
                controls
                autoPlay
                playsInline
                aria-label="Video"
                className="max-h-full max-w-full"
              />
            ) : null}
          </div>

          {caption ? (
            <p className="shrink-0 px-6 pb-6 text-center text-sm whitespace-pre-wrap text-white/90">{caption}</p>
          ) : null}
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  );
}
