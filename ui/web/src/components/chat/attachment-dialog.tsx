"use client";

import { FileText, Film, Reply, SendHorizontal, X } from "lucide-react";
import Image from "next/image";
import { useRef, type ReactNode, type RefObject } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { PendingShape, PendingStatus, type PendingUpload } from "@/lib/chat/use-pending-uploads";
import { cn } from "@/lib/utils";

export function AttachmentDialog({
  open,
  items,
  caption,
  reply,
  picker,
  busy,
  canSend,
  error,
  returnFocus,
  onCaption,
  onRemove,
  onCancel,
  onSend,
}: {
  open: boolean;
  items: PendingUpload[];
  caption: string;
  reply?: { title: string; preview: string };
  picker: ReactNode;
  busy: boolean;
  canSend: boolean;
  error: string | null;
  returnFocus: RefObject<HTMLTextAreaElement | null>;
  onCaption: (value: string) => void;
  onRemove: (id: string) => void;
  onCancel: () => void;
  onSend: () => void;
}) {
  const title = useRef<HTMLHeadingElement>(null);
  const media = items.filter((item) => item.shape !== PendingShape.File);
  const files = items.filter((item) => item.shape === PendingShape.File);
  const label = files.length === 0 ? "media"
    : media.length === 0 ? (files.length === 1 ? "file" : "files")
      : "attachments";

  return (
    <Dialog open={open} disablePointerDismissal onOpenChange={(next) => { if (!next) onCancel(); }}>
      <DialogContent
        initialFocus={title}
        finalFocus={returnFocus}
        className="flex max-h-[calc(100dvh-1.5rem)] w-[min(calc(100vw-1.5rem),26rem)] flex-col gap-3 rounded-3xl p-3 sm:p-4"
      >
        <div className="grid shrink-0 grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-1">
          <DialogClose
            render={
              <Button type="button" variant="ghost" size="icon" className="size-9 justify-self-start rounded-full" aria-label="Close attachments">
                <X aria-hidden />
              </Button>
            }
          />
          <DialogTitle ref={title} tabIndex={-1} className="text-center text-sm font-medium tabular-nums outline-none">
            {items.length ? `${items.length} ${label}` : "Attachments"}
          </DialogTitle>
          <div className="justify-self-end">{picker}</div>
        </div>

        <form className="flex min-h-0 flex-col gap-3" onSubmit={(event) => { event.preventDefault(); onSend(); }}>
          <ScrollArea className="max-h-[58dvh] flex-1">
            <ul aria-label="Attachments" className={cn("grid gap-2", media.length > 1 && "grid-cols-2")}>
              {media.map((item) => (
                <MediaPreview
                  key={item.id}
                  item={item}
                  single={media.length === 1}
                  withFiles={files.length > 0}
                  showRemove={items.length > 1}
                  onRemove={() => onRemove(item.id)}
                />
              ))}
              {files.map((item) => (
                <FilePreview key={item.id} item={item} onRemove={() => onRemove(item.id)} />
              ))}
            </ul>
          </ScrollArea>

          {reply ? (
            <div className="flex shrink-0 items-center gap-2.5 rounded-lg border-l-2 border-signal bg-muted/60 px-3 py-2">
              <Reply aria-hidden className="size-4 shrink-0 text-signal" />
              <div className="min-w-0">
                <p className="truncate text-xs font-medium text-signal">{reply.title}</p>
                <p className="truncate text-xs text-muted-foreground">{reply.preview}</p>
              </div>
            </div>
          ) : null}
          {error ? <p role="alert" className="shrink-0 px-2 text-xs text-destructive">{error}</p> : null}
          <div className="flex shrink-0 items-end gap-1 rounded-[1.6rem] border border-border bg-background/75 p-1 focus-within:border-signal/60">
            <textarea
              aria-label="Caption"
              value={caption}
              onChange={(event) => onCaption(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
                  event.preventDefault();
                  if (canSend) event.currentTarget.form?.requestSubmit();
                }
              }}
              rows={1}
              placeholder="Add a caption…"
              className="field-sizing-content max-h-[min(5.5rem,20dvh)] min-h-10 min-w-0 flex-1 resize-none bg-transparent px-3 py-2 text-base leading-6 outline-none placeholder:text-muted-foreground sm:text-sm"
            />
            <Button type="submit" variant="signal" size="icon" className="size-10 rounded-full text-signal-foreground [&>svg]:size-5" aria-label="Send" disabled={!canSend} aria-busy={busy}>
              <SendHorizontal aria-hidden />
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function MediaPreview({ item, single, withFiles, showRemove, onRemove }: {
  item: PendingUpload;
  single: boolean;
  withFiles: boolean;
  showRemove: boolean;
  onRemove: () => void;
}) {
  const singleHeight = withFiles ? "max-h-[min(45dvh,21rem)]" : "max-h-[min(52dvh,24rem)]";

  return (
    <li aria-label={item.name} className="relative min-w-0">
      <div className={cn("relative mx-auto isolate flex w-fit max-w-full justify-center overflow-hidden rounded-xl", !single && "aspect-square w-full bg-muted")}>
        {item.shape === PendingShape.Image && item.previewUrl ? (
          single ? (
            <Image src={item.previewUrl} alt="" width={400} height={400} unoptimized className={cn("h-auto w-auto max-w-full object-contain", singleHeight)} />
          ) : (
            <Image src={item.previewUrl} alt="" fill unoptimized sizes="(max-width: 416px) 45vw, 192px" className="object-cover" />
          )
        ) : (
          <div className={cn("relative flex size-full min-h-24 items-center justify-center", single && "min-w-28")}>
            <Film aria-hidden className="absolute size-7 text-muted-foreground" />
            <video
              src={item.previewUrl ? `${item.previewUrl}#t=0.1` : undefined}
              muted
              playsInline
              preload="metadata"
              controls={single}
              aria-label={`Preview ${item.name}`}
              className={cn("relative", single ? ["max-w-full object-contain", singleHeight] : "size-full object-cover")}
            />
          </div>
        )}
        {showRemove || item.status === PendingStatus.Failed ? (
          <RemoveAttachment name={item.name} onRemove={onRemove} className="absolute top-1.5 right-1.5 bg-popover/95" />
        ) : null}
      </div>
      {item.status !== PendingStatus.Ready ? (
        <div className="px-1 pt-1.5"><AttachmentStatus item={item} /></div>
      ) : null}
    </li>
  );
}

function FilePreview({ item, onRemove }: { item: PendingUpload; onRemove: () => void }) {
  return (
    <li aria-label={item.name} className="col-span-full flex min-w-0 items-center gap-3 rounded-xl bg-background/65 py-1.5 pr-1.5 pl-2.5">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-signal/12 text-signal">
        <FileText aria-hidden className="size-4" />
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm" title={item.name}>{item.name}</p>
        <AttachmentStatus item={item} />
      </div>
      <RemoveAttachment name={item.name} onRemove={onRemove} />
    </li>
  );
}

function AttachmentStatus({ item }: { item: PendingUpload }) {
  if (item.status === PendingStatus.Failed) {
    return <p role="alert" className="text-xs break-words text-destructive">{item.error || "Upload failed"}</p>;
  }
  if (item.status !== PendingStatus.Uploading) return null;
  const percent = Math.round(item.progress * 100);
  if (item.hashing || item.processing || item.progress >= 1) {
    return <p role="status" className="text-xs text-muted-foreground">{item.hashing ? `Checking file… ${percent}%` : "Processing…"}</p>;
  }
  return <progress aria-label={`Uploading ${item.name}`} max={100} value={percent} className="block h-1.5 w-full accent-signal" />;
}

function RemoveAttachment({ name, onRemove, className }: { name: string; onRemove: () => void; className?: string }) {
  return (
    <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${name}`} className={cn("size-8 rounded-full", className)} onClick={onRemove}>
      <X aria-hidden />
    </Button>
  );
}
