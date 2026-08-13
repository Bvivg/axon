"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";

import { Transcript } from "@/components/chat/transcript";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { describe } from "@/lib/errors";
import { useHistory, useRoom } from "@/lib/query/chat";
import { useChatSocket } from "@/lib/ws/use-chat-socket";

export default function ChatRoomPage() {
  const params = useParams<{ roomId: string }>();
  const roomID = params.roomId;

  const { status, user } = useSession();

  useRequireSession();

  const room = useRoom(roomID);
  const history = useHistory(roomID);

  const socket = useChatSocket(roomID, history.data);

  const [draft, setDraft] = useState("");

  const names = useMemo(() => {
    const byID = new Map<string, string>();
    for (const member of room.data?.members ?? []) {
      if (member.displayName) {
        byID.set(member.userId, member.displayName);
      }
    }
    return byID;
  }, [room.data]);

  const askedAbout = useRef(new Set<string>());
  const newestAuthor = socket.messages.at(-1)?.author_id;
  useEffect(() => {
    if (!newestAuthor || names.has(newestAuthor) || askedAbout.current.has(newestAuthor)) {
      return;
    }
    askedAbout.current.add(newestAuthor);
    void room.refetch();
  }, [newestAuthor, names, room]);

  if (status !== "authenticated" || !user) {
    return (
      <main className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </main>
    );
  }

  const onSend = (event: FormEvent) => {
    event.preventDefault();
    if (socket.send(draft)) {
      setDraft("");
    }
  };

  return (

    <main className="mx-auto flex h-[calc(100dvh-7rem)] w-full max-w-2xl flex-col gap-4 px-6 py-8 md:h-screen">
      <header className="flex items-baseline justify-between">
        <div>
          <h1 className="text-xl font-semibold">
            {room.data?.room?.name ?? "Room"}
          </h1>
          <p className="text-xs text-muted-foreground">
            {socket.connected ? "Connected" : "Reconnecting…"}
            {room.data?.room ? ` · ${room.data.room.memberCount} in the room` : null}
          </p>
        </div>
        <Link className="text-sm underline underline-offset-4" href="/chat">
          All rooms
        </Link>
      </header>

      {room.isError ? <Alert>{describe(room.error)}</Alert> : null}
      {history.isError ? <Alert>{describe(history.error)}</Alert> : null}
      {socket.error ? <Alert>{socket.error}</Alert> : null}

      <Transcript messages={socket.messages} selfID={user.id} names={names} />

      <form className="flex items-center gap-3" onSubmit={onSend}>
        <Input
          aria-label="Message"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          placeholder={socket.connected ? "Say something" : "Waiting for the connection…"}
          disabled={!socket.connected}
        />
        <Button type="submit" disabled={!socket.connected || draft.trim() === ""}>
          Send
        </Button>
      </form>

      <p className="text-xs text-muted-foreground">
        Room id: <code className="font-mono">{roomID}</code>
      </p>
    </main>
  );
}
