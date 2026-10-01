"use client";

import { FileText, Film, ImageIcon, Mic, Paperclip, Pencil, Reply, SendHorizontal, Trash2, X } from "lucide-react";
import Image from "next/image";
import { useEffect, useRef, useState, type ClipboardEvent, type FormEvent, type KeyboardEvent } from "react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { UploadAs, UploadCancelled, uploadAttachment } from "@/lib/chat/upload";
import {
  PendingShape,
  PendingStatus,
  type PendingUpload,
  type usePendingUploads,
} from "@/lib/chat/use-pending-uploads";
import { RecorderState, useVoiceRecorder } from "@/lib/chat/use-voice-recorder";
import { formatDuration } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { SendOptions } from "@/lib/ws/use-chat-socket";

type PendingUploads = ReturnType<typeof usePendingUploads>;

export enum ComposerMode {
  Reply = "reply",
  Edit = "edit",
}

export interface ComposerContext {
  mode: ComposerMode;
  messageId: string;
  title: string;
  preview: string;
  body: string;
  bodyOptional?: boolean;
}

export function MessageComposer({
  connected,
  uploads,
  context,
  onSend,
  onEdit,
  onCancelContext,
  onTyping,
}: {
  connected: boolean;
  uploads: PendingUploads;
  context: ComposerContext | null;
  onSend: (body: string, options?: SendOptions) => boolean;
  onEdit: (messageId: string, body: string, onSettled?: (saved: boolean) => void) => boolean;
  onCancelContext: () => void;
  onTyping: () => void;
}) {
  const [draft, setDraft] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const editing = context?.mode === ComposerMode.Edit;
  const replyToId = context?.mode === ComposerMode.Reply ? context.messageId : undefined;

  const contextKey = context ? `${context.mode}:${context.messageId}` : "";
  const [appliedContext, setAppliedContext] = useState("");
  const [stashedDraft, setStashedDraft] = useState("");
  const [saving, setSaving] = useState(false);
  if (contextKey !== appliedContext) {
    const wasEditing = appliedContext.startsWith(`${ComposerMode.Edit}:`);
    setAppliedContext(contextKey);
    setSaving(false);
    if (editing) {
      if (!wasEditing) {
        setStashedDraft(draft);
      }
      setDraft(context.body);
    } else if (wasEditing) {
      setDraft(stashedDraft);
      setStashedDraft("");
    }
  }

  const currentContext = useRef(contextKey);
  useEffect(() => {
    currentContext.current = contextKey;
  });

  useEffect(() => {
    if (contextKey) {
      input.current?.focus();
    }
  }, [contextKey]);
  const [voiceError, setVoiceError] = useState<string | null>(null);
  const [voiceSending, setVoiceSending] = useState(false);
  const mediaInput = useRef<HTMLInputElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const recorder = useVoiceRecorder();

  const hasUploads = uploads.items.length > 0;
  const canSend = editing
    ? connected && !saving && (context.bodyOptional || draft.trim() !== "") && draft !== context.body
    : connected && !uploads.busy && (draft.trim() !== "" || uploads.ready.length > 0);

  const cancelContext = () => {
    onCancelContext();
  };

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (!canSend) {
      return;
    }

    if (editing) {
      const editedContext = contextKey;
      const sent = onEdit(context.messageId, draft, (saved) => {
        if (currentContext.current !== editedContext) {
          return;
        }
        setSaving(false);
        if (saved) {
          onCancelContext();
        }
      });
      setSaving(sent);
      return;
    }

    if (uploads.ready.length === 0) {
      if (onSend(draft, { replyToId })) {
        setDraft("");
        onCancelContext();
      }
      return;
    }

    const sent: string[] = [];
    uploads.ready.forEach((item, index) => {
      const first = index === 0;
      const options = { uploadId: item.uploaded?.uploadId, replyToId: first ? replyToId : undefined };
      if (item.uploaded && onSend(first ? draft : "", options)) {
        sent.push(item.id);
      }
    });
    if (sent.length > 0) {
      uploads.take(sent);
      setDraft("");
      onCancelContext();
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Escape" && context) {
      event.preventDefault();
      cancelContext();
    }
  };

  const onDraftChange = (value: string) => {
    setDraft(value);
    if (value.trim() !== "") {
      onTyping();
    }
  };

  const onPaste = (event: ClipboardEvent<HTMLInputElement>) => {
    const files = [...event.clipboardData.files];
    if (files.length > 0) {
      event.preventDefault();
      uploads.add(files);
    }
  };

  const pick = (input: HTMLInputElement | null, as: UploadAs) => {
    const files = input?.files ? [...input.files] : [];
    if (input) input.value = "";
    if (files.length > 0) {
      uploads.add(files, as);
    }
  };

  const startVoice = () => {
    setVoiceError(null);
    void recorder.start();
  };

  const sendVoice = async () => {
    setVoiceSending(true);
    const recording = await recorder.stop();
    if (!recording) {
      setVoiceSending(false);
      return;
    }
    try {
      const uploaded = await uploadAttachment(recording.blob, recording.filename, UploadAs.Voice);
      if (onSend("", { uploadId: uploaded.uploadId, replyToId })) {
        onCancelContext();
      } else {
        setVoiceError("The voice message was not sent. Check the connection and try again.");
      }
    } catch (err) {
      if (!(err instanceof UploadCancelled)) {
        setVoiceError(err instanceof Error ? err.message : "The voice message was not sent.");
      }
    } finally {
      setVoiceSending(false);
    }
  };

  const recording = recorder.state !== RecorderState.Idle;
  const failed = uploads.items.filter((item) => item.status === PendingStatus.Failed);
  const error = voiceError ?? recorder.error;

  return (
    <div className="shrink-0 pt-3">
      {context ? (
        <div className="mb-2 flex items-center gap-3 rounded-lg border-l-2 border-signal bg-muted/60 py-1.5 pr-1 pl-3">
          {editing ? (
            <Pencil aria-hidden className="size-4 shrink-0 text-signal" />
          ) : (
            <Reply aria-hidden className="size-4 shrink-0 text-signal" />
          )}
          <div className="min-w-0 flex-1">
            <p className="truncate text-xs font-medium text-signal">{context.title}</p>
            <p className="truncate text-xs text-muted-foreground">{context.preview}</p>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={editing ? "Cancel editing" : "Cancel reply"}
            onClick={cancelContext}
          >
            <X />
          </Button>
        </div>
      ) : null}

      {hasUploads && !editing ? (
        <ScrollArea orientation="horizontal" className="mb-2">
          <ul aria-label="Attachments" className="flex w-max gap-2 pb-3">
            {uploads.items.map((item) => (
              <PendingChip key={item.id} item={item} onRemove={() => uploads.remove(item.id)} />
            ))}
          </ul>
        </ScrollArea>
      ) : null}

      {failed.map((item) => (
        <p key={item.id} role="alert" className="mb-2 text-xs text-destructive">
          {item.name}: {item.error}
        </p>
      ))}
      {error ? (
        <p role="alert" className="mb-2 text-xs text-destructive">
          {error}
        </p>
      ) : null}

      {recording || voiceSending ? (
        <div className="flex items-center gap-3">
          <Button
            variant="ghost"
            size="icon"
            aria-label="Cancel recording"
            onClick={recorder.cancel}
            disabled={voiceSending}
          >
            <Trash2 />
          </Button>
          <div className="flex h-8 flex-1 items-center gap-2 rounded-md border border-border px-3 text-sm">
            <span
              aria-hidden
              className={cn("size-2 rounded-full bg-destructive", voiceSending ? "opacity-40" : "animate-pulse")}
            />
            <span role="status" className="font-mono text-xs text-muted-foreground">
              {voiceSending ? "Sending…" : `Recording ${formatDuration(recorder.elapsedMs)}`}
            </span>
          </div>
          <Button
            variant="signal"
            aria-label="Send voice message"
            onClick={() => void sendVoice()}
            disabled={voiceSending || recorder.state !== RecorderState.Recording || !connected}
            aria-busy={voiceSending}
          >
            <SendHorizontal />
          </Button>
        </div>
      ) : (
        <form className="flex items-center gap-2" onSubmit={onSubmit}>
          {editing ? null : (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <Button variant="ghost" size="icon" aria-label="Attach" disabled={!connected}>
                    <Paperclip />
                  </Button>
                }
              />
              <DropdownMenuContent side="top" className="w-48">
                <DropdownMenuItem onClick={() => mediaInput.current?.click()}>
                  <ImageIcon />
                  Photo or video
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => fileInput.current?.click()}>
                  <FileText />
                  File
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          <input
            ref={mediaInput}
            type="file"
            accept="image/*,video/*"
            multiple
            hidden
            aria-label="Photos or videos"
            onChange={(event) => pick(event.currentTarget, UploadAs.Media)}
          />
          <input
            ref={fileInput}
            type="file"
            multiple
            hidden
            aria-label="Files"
            onChange={(event) => pick(event.currentTarget, UploadAs.File)}
          />

          <Input
            ref={input}
            aria-label="Message"
            value={draft}
            onChange={(event) => onDraftChange(event.target.value)}
            onPaste={editing ? undefined : onPaste}
            onKeyDown={onKeyDown}
            placeholder={
              !connected
                ? "Waiting for the connection…"
                : editing
                  ? "Edit the message"
                  : hasUploads
                    ? "Add a caption"
                    : "Say something"
            }
            disabled={!connected}
          />

          {draft.trim() === "" && !hasUploads && !editing ? (
            <Button
              variant="ghost"
              size="icon"
              aria-label="Record voice message"
              onClick={startVoice}
              disabled={!connected}
            >
              <Mic />
            </Button>
          ) : null}
          <Button type="submit" variant="signal" disabled={!canSend} aria-busy={editing ? saving : uploads.busy}>
            {editing ? "Save" : "Send"}
          </Button>
        </form>
      )}
    </div>
  );
}

function PendingChip({ item, onRemove }: { item: PendingUpload; onRemove: () => void }) {
  const failed = item.status === PendingStatus.Failed;
  const uploading = item.status === PendingStatus.Uploading;

  return (
    <li
      aria-label={item.name}
      className={cn(
        "relative flex h-14 max-w-56 shrink-0 items-center gap-2 rounded-lg border bg-card pr-8 pl-1.5",
        failed ? "border-destructive" : "border-border",
      )}
    >
      <span className="relative flex size-11 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted text-muted-foreground">
        {item.shape === PendingShape.Image && item.previewUrl ? (
          <Image src={item.previewUrl} alt="" fill unoptimized sizes="44px" className="object-cover" />
        ) : item.shape === PendingShape.Video ? (
          <Film aria-hidden className="size-5" strokeWidth={1.5} />
        ) : (
          <FileText aria-hidden className="size-5" strokeWidth={1.5} />
        )}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-xs font-medium">{item.name}</span>
        {uploading && item.progress >= 1 ? (
          <span role="status" className="block text-[10px] text-muted-foreground">
            Processing…
          </span>
        ) : uploading ? (
          <span
            role="progressbar"
            aria-label={`Uploading ${item.name}`}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round(item.progress * 100)}
            className="mt-1.5 block h-1 w-24 overflow-hidden rounded-full bg-muted"
          >
            <span
              className="block h-full rounded-full bg-signal transition-[width] duration-200"
              style={{ width: `${Math.max(4, item.progress * 100)}%` }}
            />
          </span>
        ) : (
          <span className={cn("block text-[10px]", failed ? "text-destructive" : "text-muted-foreground")}>
            {failed ? "Failed" : "Ready"}
          </span>
        )}
      </span>
      <button
        type="button"
        onClick={onRemove}
        aria-label={`Remove ${item.name}`}
        className="absolute top-1 right-1 flex size-6 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
      >
        <X aria-hidden className="size-3.5" />
      </button>
    </li>
  );
}
