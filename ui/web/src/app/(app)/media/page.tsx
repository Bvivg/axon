"use client";

import { Images } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { RoomMediaLibrary } from "@/components/chat/room-media";
import { useRoomDirectory } from "@/components/chat/use-room-directory";
import { WorkspacePage } from "@/components/nav/workspace-page";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { useSession } from "@/lib/auth/session";
import { chatHref } from "@/lib/chat/address";
import { profileName } from "@/lib/format";
import { useRoom } from "@/lib/query/chat";
import { usePublicProfiles } from "@/lib/query/people";

export default function MediaPage() {
  const { user } = useSession();
  const { rooms, entries } = useRoomDirectory();
  const [roomID, setRoomID] = useState("");
  const selected = entries.find((entry) => entry.room.id === roomID) ?? entries[0];
  const room = useRoom(selected?.room.id ?? "");
  const profiles = usePublicProfiles(room.data?.members.map(({ userId }) => userId) ?? []);
  const names = new Map<string, string>();
  profiles.data?.forEach((profile, id) => names.set(id, profileName(profile)));
  if (selected?.peer) names.set(selected.peer.id, profileName(selected.peer));

  return (
    <WorkspacePage title="Media" back="/chat" backLabel="Back to chats">
      {rooms.isPending ? <p role="status" className="text-muted-foreground">Loading chats…</p> : null}
      {rooms.isError ? <Alert>Could not load chats. <Button variant="outline" onClick={() => void rooms.refetch()}>Retry</Button></Alert> : null}
      {rooms.isSuccess && !entries.length ? (
        <div className="rounded-[14px] border border-line bg-surface"><EmptyState icon={Images} title="No shared media yet"><Link href="/chat" className="text-signal underline">Open chats</Link></EmptyState></div>
      ) : null}
      {selected && user ? (
        <section aria-labelledby="shared-by-chat" className="rounded-[14px] border border-line bg-surface">
          <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
            <h2 id="shared-by-chat" className="text-sm font-semibold">Shared by chat</h2>
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <select aria-label="Choose a chat" value={selected.room.id} onChange={(event) => setRoomID(event.target.value)} className="min-h-10 max-w-full rounded-lg border border-border bg-background px-2 text-xs">
                {entries.map((entry) => <option key={entry.room.id} value={entry.room.id}>{entry.label}</option>)}
              </select>
              <Link href={chatHref(selected.address)} className="rounded-lg px-2 py-2 text-xs text-signal underline">Open chat</Link>
            </div>
          </header>
          <div className="px-4 py-4"><RoomMediaLibrary key={selected.room.id} roomID={selected.room.id} names={names} selfID={user.id} /></div>
        </section>
      ) : null}
    </WorkspacePage>
  );
}
