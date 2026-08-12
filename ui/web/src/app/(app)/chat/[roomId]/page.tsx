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

/**
 * One room.
 *
 * Two sources feed it, and the split is deliberate. History comes from
 * ListMessages through the query cache — a page of what was said before this
 * tab existed. Everything from then on arrives on the socket. Neither one
 * refetches the other: a client that re-read history every time a message
 * arrived would be asking the server for what it had just been handed.
 */
export default function ChatRoomPage() {
  const params = useParams<{ roomId: string }>();
  const roomID = params.roomId;

  const { status, user } = useSession();

  useRequireSession();

  const room = useRoom(roomID);
  const history = useHistory(roomID);

  // The socket is handed the history page as it lands, so the transcript holds
  // both halves and the reconnect position is right from the moment there is
  // one to be right about.
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

  /**
   * Somebody who joined after this page fetched the member list has no name in
   * it yet — membership arrives on no channel this page otherwise watches (see
   * the comment on useRoom). The newest message is the signal that this page's
   * copy might be stale, and a stranger sending it is worth one refetch to find
   * out who they are.
   *
   * Only the newest author, not every message: a name missing further back is
   * as likely to be somebody who has since left as somebody this page has not
   * heard of yet, and Transcript already has an honest fallback for that.
   * askedAbout keeps this to one attempt per stranger rather than one per
   * message they send while the refetch is in flight.
   */
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
    // The floating bottom bar (see AppShell) reserves its own space on
    // mobile, so a plain h-screen would push the composer behind it — 7rem
    // matches the wrapper's own clearance (pb-28) exactly for that reason.
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

      {/* The id is how somebody else gets in: rooms are open, and this is the
          invitation. */}
      <p className="text-xs text-muted-foreground">
        Room id: <code className="font-mono">{roomID}</code>
      </p>
    </main>
  );
}
