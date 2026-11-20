"use client";

import { FileText, ImageIcon, Mic, Paperclip, Pencil, Plus, Reply, SendHorizontal, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState, type ClipboardEvent, type FormEvent, type KeyboardEvent } from "react";

import { AttachmentDialog } from "@/components/chat/attachment-dialog";
import { EmojiPicker } from "@/components/chat/emoji-picker";
import { useChatDrafts } from "@/components/chat/chat-activity";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Textarea } from "@/components/ui/textarea";
import { messageLengthError, messageTooLong } from "@/lib/chat/message-limits";
import { useChatAddress } from "@/lib/chat/address";
import { UploadAs, UploadCancelled, uploadAttachment } from "@/lib/chat/upload";
import {
  PendingStatus,
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
  const address = useChatAddress();
  const drafts = useChatDrafts();
  const [draft, setDraft] = useState(() => drafts?.get(address) ?? "");
  const input = useRef<HTMLTextAreaElement>(null);
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
  const [attachmentError, setAttachmentError] = useState<string | null>(null);
  const mediaInput = useRef<HTMLInputElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const recorder = useVoiceRecorder();

  const recording = recorder.state !== RecorderState.Idle;
  const attachmentsOpen = uploads.items.length > 0 && !editing && !recording && !voiceSending;
  const canSendText = editing
    ? connected && !saving && (context.bodyOptional || draft.trim() !== "") && draft !== context.body
    : connected && !attachmentsOpen && draft.trim() !== "";
  const canSendAttachments = connected && !uploads.busy && uploads.items.length > 0
    && uploads.items.every((item) => item.status === PendingStatus.Ready);

  const cancelContext = () => {
    onCancelContext();
  };

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    submit();
  };

  const submit = () => {
    if (!canSendText) {
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

    if (onSend(draft, { replyToId })) {
      drafts?.delete(address);
      setDraft("");
      onCancelContext();
    }
  };

  const submitAttachments = () => {
    if (!attachmentsOpen || !canSendAttachments) return;
    if (messageTooLong(draft)) {
      setAttachmentError(messageLengthError);
      return;
    }

    setAttachmentError(null);
    const uploadIds = uploads.items.flatMap((item) => item.uploaded ? [item.uploaded.uploadId] : []);
    if (uploadIds.length !== uploads.items.length || !onSend(draft, { uploadIds, replyToId })) {
      setAttachmentError("Couldn’t send the attachments. Try again when connected.");
      return;
    }
    uploads.take(uploads.items.map((item) => item.id));
    drafts?.delete(address);
    setDraft("");
    onCancelContext();
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Escape" && context) {
      event.preventDefault();
      cancelContext();
      return;
    }
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      submit();
    }
  };

  const onDraftChange = (value: string) => {
    setDraft(value);
    if (!editing) {
      if (value.trim() !== "") drafts?.set(address, value);
      else drafts?.delete(address);
    }
    setAttachmentError(null);
    if (value.trim() !== "") {
      onTyping();
    }
  };

  const insertEmoji = (emoji: string) => {
    const field = input.current;
    const start = field?.selectionStart ?? draft.length;
    const end = field?.selectionEnd ?? draft.length;
    onDraftChange(draft.slice(0, start) + emoji + draft.slice(end));
    requestAnimationFrame(() => {
      field?.focus();
      field?.setSelectionRange(start + emoji.length, start + emoji.length);
    });
  };

  const onPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
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

  const attachmentMenu = (inDialog: boolean) => (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size={inDialog ? "sm" : "icon"}
            aria-label={inDialog ? "Add more" : "Attach"}
            disabled={!connected}
            className={inDialog ? "min-h-9 gap-1 rounded-full px-2 text-signal [&>svg]:size-3.5" : "chat-composer-circle"}
          >
            {inDialog ? <Plus /> : <Paperclip aria-hidden />}
            {inDialog ? "Add more" : null}
          </Button>
        }
      />
      <DropdownMenuContent side={inDialog ? "bottom" : "top"} align={inDialog ? "end" : "start"} className="w-48">
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
  );

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

  const error = voiceError ?? recorder.error ?? (attachmentsOpen ? null : uploads.selectionError);

  return (
    <div className="chat-composer-card shrink-0">
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

      <AttachmentDialog
        open={attachmentsOpen}
        items={uploads.items}
        caption={draft}
        reply={context?.mode === ComposerMode.Reply ? context : undefined}
        picker={attachmentMenu(true)}
        busy={uploads.busy}
        canSend={canSendAttachments}
        error={attachmentError ?? uploads.selectionError ?? (!connected ? "Waiting for the connection…" : null)}
        returnFocus={input}
        onCaption={onDraftChange}
        onRemove={(id) => {
          setAttachmentError(null);
          uploads.remove(id);
        }}
        onCancel={() => {
          setAttachmentError(null);
          uploads.clear();
        }}
        onSend={submitAttachments}
      />
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
        <form className="chat-composer-row" onSubmit={onSubmit}>
          {editing ? null : attachmentMenu(false)}
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

          <div className="chat-composer-field">
          <Textarea
            ref={input}
            aria-label="Message"
            rows={1}
            className="chat-composer-input"
            value={attachmentsOpen ? "" : draft}
            onChange={(event) => onDraftChange(event.target.value)}
            onPaste={editing ? undefined : onPaste}
            onKeyDown={onKeyDown}
            placeholder={
              !connected
                ? "Waiting for the connection…"
                : editing
                  ? "Edit the message"
                  : "Message…"
            }
          />
          <EmojiPicker onPick={insertEmoji} returnFocus={input} />
          </div>

          {draft.trim() === "" && !editing ? (
            <Button
              variant="ghost"
              size="icon"
              aria-label="Record voice message"
              className="chat-composer-circle"
              onClick={startVoice}
              disabled={!connected}
            >
              <Mic aria-hidden />
            </Button>
          ) : <Button type="submit" variant="signal" size={editing ? "default" : "icon"} className={cn("chat-composer-send", !editing && "chat-composer-circle")} aria-label={editing ? "Save" : "Send"} disabled={!canSendText} aria-busy={editing && saving}>
            {editing ? "Save" : <SendHorizontal aria-hidden />}
          </Button>}
        </form>
      )}
    </div>
  );
}
