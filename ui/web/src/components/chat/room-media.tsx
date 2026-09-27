"use client";

import { AudioLines, FileText, ImageIcon, Play, X } from "lucide-react";
import Image from "next/image";
import { useMemo, useState } from "react";

import { MediaViewer } from "@/components/chat/media-viewer";
import { FileCard, VoicePlayer } from "@/components/chat/message-content";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { MessageKind } from "@/gen/axon/chat/v1/chat_pb";
import { ContentKind, contentOf } from "@/lib/chat/content";
import { describe } from "@/lib/errors";
import { formatDuration } from "@/lib/format";
import { useRoomMedia } from "@/lib/query/chat";
import type { WireMessage } from "@/lib/ws/protocol";

enum Section {
  Media = "media",
  Files = "files",
  Voice = "voice",
}

const mediaKinds = [MessageKind.IMAGE, MessageKind.VIDEO];
const fileKinds = [MessageKind.ATTACHMENT];
const voiceKinds = [MessageKind.VOICE];

export function RoomMedia({
  roomID,
  names,
  selfID,
  open,
  onOpenChange,
}: {
  roomID: string;
  names: Map<string, string>;
  selfID: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(40rem,calc(100dvh-2rem))] w-[min(calc(100vw-2rem),36rem)] flex-col gap-4">
        <div className="flex items-center justify-between gap-3">
          <DialogTitle>Media</DialogTitle>
          <DialogClose render={<Button variant="ghost" size="icon" aria-label="Close" />}>
            <X />
          </DialogClose>
        </div>
        {open ? (
          <Tabs defaultValue={Section.Media} className="flex min-h-0 flex-1 flex-col gap-3">
            <TabsList className="self-start">
              <TabsTrigger value={Section.Media}>Media</TabsTrigger>
              <TabsTrigger value={Section.Files}>Files</TabsTrigger>
              <TabsTrigger value={Section.Voice}>Voice</TabsTrigger>
            </TabsList>
            <TabsContent value={Section.Media} className="flex min-h-0 flex-1 flex-col">
              <ScrollArea className="flex-1">
                <MediaGrid roomID={roomID} />
              </ScrollArea>
            </TabsContent>
            <TabsContent value={Section.Files} className="flex min-h-0 flex-1 flex-col">
              <ScrollArea className="flex-1">
                <MessageList roomID={roomID} kinds={fileKinds} names={names} selfID={selfID} />
              </ScrollArea>
            </TabsContent>
            <TabsContent value={Section.Voice} className="flex min-h-0 flex-1 flex-col">
              <ScrollArea className="flex-1">
                <MessageList roomID={roomID} kinds={voiceKinds} names={names} selfID={selfID} />
              </ScrollArea>
            </TabsContent>
          </Tabs>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function useNewestFirst(roomID: string, kinds: MessageKind[]) {
  const query = useRoomMedia(roomID, kinds);
  const messages = useMemo(
    () => query.data?.pages.flatMap((page) => [...page.messages].reverse()) ?? [],
    [query.data],
  );
  return { query, messages };
}

function LoadMore({ query }: { query: ReturnType<typeof useRoomMedia> }) {
  if (!query.hasNextPage) {
    return null;
  }
  return (
    <div className="flex justify-center pt-3">
      <Button
        variant="outline"
        size="sm"
        onClick={() => void query.fetchNextPage()}
        disabled={query.isFetchingNextPage}
        aria-busy={query.isFetchingNextPage}
      >
        Load more
      </Button>
    </div>
  );
}

function MediaGrid({ roomID }: { roomID: string }) {
  const { query, messages } = useNewestFirst(roomID, mediaKinds);
  const [viewing, setViewing] = useState<WireMessage | null>(null);

  if (query.isError) {
    return <Alert>{describe(query.error)}</Alert>;
  }
  if (query.isPending) {
    return (
      <div className="grid grid-cols-3 gap-1 sm:grid-cols-4">
        {Array.from({ length: 8 }, (_, index) => (
          <Skeleton key={index} className="aspect-square rounded-md" />
        ))}
      </div>
    );
  }
  if (messages.length === 0) {
    return <EmptyState icon={ImageIcon} title="No photos or videos yet" />;
  }

  return (
    <>
      <ul aria-label="Photos and videos" className="grid grid-cols-3 gap-1 sm:grid-cols-4">
        {messages.map((message) => {
          const content = contentOf(message);
          if (content.kind === ContentKind.Image) {
            return (
              <li key={message.id}>
                <button
                  type="button"
                  onClick={() => setViewing(message)}
                  aria-label="Open photo"
                  className="relative block aspect-square w-full overflow-hidden rounded-md bg-muted"
                >
                  <Image
                    src={content.image.thumbnailUrl}
                    alt=""
                    fill
                    unoptimized
                    sizes="150px"
                    className="object-cover"
                  />
                </button>
              </li>
            );
          }
          if (content.kind === ContentKind.Video) {
            return (
              <li key={message.id}>
                <button
                  type="button"
                  onClick={() => setViewing(message)}
                  aria-label="Play video"
                  className="relative block aspect-square w-full overflow-hidden rounded-md bg-black"
                >
                  {content.video.posterUrl ? (
                    <Image
                      src={content.video.posterUrl}
                      alt=""
                      fill
                      unoptimized
                      sizes="150px"
                      className="object-cover"
                    />
                  ) : null}
                  <span className="absolute bottom-1 left-1 flex items-center gap-1 rounded bg-black/55 px-1 py-0.5 font-mono text-[10px] text-white">
                    <Play aria-hidden className="size-2.5 fill-current" />
                    {formatDuration(content.video.durationMs)}
                  </span>
                </button>
              </li>
            );
          }
          return null;
        })}
      </ul>
      <LoadMore query={query} />
      <MediaViewer message={viewing} onClose={() => setViewing(null)} />
    </>
  );
}

function MessageList({
  roomID,
  kinds,
  names,
  selfID,
}: {
  roomID: string;
  kinds: MessageKind[];
  names: Map<string, string>;
  selfID: string;
}) {
  const { query, messages } = useNewestFirst(roomID, kinds);
  const voice = kinds.includes(MessageKind.VOICE);

  if (query.isError) {
    return <Alert>{describe(query.error)}</Alert>;
  }
  if (query.isPending) {
    return (
      <div className="space-y-2">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-12 rounded-md" />
        ))}
      </div>
    );
  }
  if (messages.length === 0) {
    return voice ? (
      <EmptyState icon={AudioLines} title="No voice messages yet" />
    ) : (
      <EmptyState icon={FileText} title="No files yet" />
    );
  }

  return (
    <>
      <ul aria-label={voice ? "Voice messages" : "Files"} className="divide-y divide-border">
        {messages.map((message) => {
          const content = contentOf(message);
          const author = message.author_id === selfID ? "You" : (names.get(message.author_id) ?? "Someone");
          return (
            <li key={message.id} className="flex flex-col gap-1 py-2.5">
              <span className="text-xs text-muted-foreground">
                {author} · {formatDate(message.sent_at)}
              </span>
              {content.kind === ContentKind.File ? (
                <FileCard file={content.file} mine={false} />
              ) : content.kind === ContentKind.Voice ? (
                <VoicePlayer voice={content.voice} mine={false} />
              ) : null}
            </li>
          );
        })}
      </ul>
      <LoadMore query={query} />
    </>
  );
}

function formatDate(iso: string): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) {
    return "";
  }
  return at.toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
}
