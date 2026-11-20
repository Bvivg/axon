"use client";

import { ArrowLeft, SquarePen } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

import { DirectoryList } from "@/components/chat/directory-list";
import { ChatComposer } from "@/components/chat/chat-composer";
import { PersonRow } from "@/components/chat/person-row";
import { CallButtons } from "@/components/calls/call-buttons";
import { RoomList } from "@/components/chat/room-list";
import { RoomListItem } from "@/components/chat/room-list-item";
import { ListSearchField } from "@/components/ui/list-search-field";
import { setSidebarMode, SidebarMode, useChatSidebar } from "@/components/chat/sidebar-store";
import { useRoomDirectory } from "@/components/chat/use-room-directory";
import { Button } from "@/components/ui/button";
import { ListColumnHeader } from "@/components/ui/list-column";
import { useSession } from "@/lib/auth/session";
import { isRoomAt, openChat, useChatAddress } from "@/lib/chat/address";
import { useDebouncedValue } from "@/lib/hooks/use-debounced-value";
import { useSearchUsers } from "@/lib/query/people";

export function ChatSidebar() {
  const { mode, focus } = useChatSidebar();
  const [query, setQuery] = useState("");

  const done = () => {
    setQuery("");
    setSidebarMode(SidebarMode.Chats);
  };

  if (mode === SidebarMode.Compose) return <ChatComposer onDone={done} />;

  return (
    <>
      <ListColumnHeader
        title="Chats"
        back={mode === SidebarMode.Search ? <Button variant="ghost" size="icon" aria-label="Back to chats" onClick={done}><ArrowLeft /></Button> : null}
        action={<Button variant="signal" size="icon" className="size-9 text-signal-foreground hover:text-signal-foreground" aria-label="New chat" onClick={() => setSidebarMode(SidebarMode.Compose)}><SquarePen /></Button>}
      />
      <ListSearchField
        label="Search chats and people"
        value={query}
        onFocus={() => { if (mode !== SidebarMode.Search) setSidebarMode(SidebarMode.Search); }}
        onChange={setQuery}
        focus={mode === SidebarMode.Search ? focus : undefined}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            if (query) setQuery("");
            else { done(); event.currentTarget.blur(); }
          }
          if (event.key === "ArrowDown") {
            event.preventDefault();
            document.querySelector<HTMLElement>('[aria-label="Chat search results"] a, [aria-label="Chat search results"] button')?.focus();
          }
        }}
      />
      <div className="flex min-h-0 flex-1 flex-col" onClick={(event) => {
        if ((event.target as Element).closest("a[data-room-id]")) done();
      }}>
        {mode === SidebarMode.Search || query.trim() ? <ChatSearch query={query} onDone={done} /> : <RoomList />}
      </div>
    </>
  );
}

function ChatSearch({ query, onDone }: { query: string; onDone: () => void }) {
  const router = useRouter();
  const { user } = useSession();
  const address = useChatAddress();
  const { entries, rooms } = useRoomDirectory();
  const needle = query.trim().toLowerCase().replace(/^[@#]/, "");
  const debounced = useDebouncedValue(needle, 350);
  const search = useSearchUsers(debounced);
  const people = debounced === needle ? (search.data ?? []).filter((person) => person.id !== user?.id && person.username) : [];
  const foundIDs = new Set(people.map((person) => person.id));
  const chats = entries.filter((entry) => needle !== "" && (
    entry.label.toLowerCase().includes(needle) ||
    entry.peer?.username?.toLowerCase().includes(needle) ||
    (!!entry.room.peerUserId && foundIDs.has(entry.room.peerUserId))
  ));
  const existingPeers = new Set(chats.map((entry) => entry.room.peerUserId));
  const found = people.filter((person) => !existingPeers.has(person.id));
  const loading = debounced !== needle || search.isPending || rooms.isLoading;
  return (
    <DirectoryList
      key={needle}
      label="Chat search results"
      search={search}
      loading={loading}
      rooms={rooms}
      empty="No chats or people found."
      sections={[
        { label: "Matching chats", title: "Chats", rows: chats.map((entry) => ({
          id: entry.room.id,
          height: 56,
          content: <RoomListItem room={entry.room} peerProfile={entry.peer} active={isRoomAt(address, entry.room, entry.peer)} />,
        })) },
        { label: "Matching people", title: "People", rows: found.map((profile) => ({
          id: profile.id,
          height: 56,
          content: <div className="flex min-w-0 items-center pr-2">
            <div className="min-w-0 flex-1">
              <PersonRow profile={profile} onPick={(person) => {
                openChat(router, person.username!);
                onDone();
              }} />
            </div>
            <CallButtons target={{ peerUserId: profile.id }} name={profile.displayName || profile.username} onStart={onDone} />
          </div>,
        })) },
      ]}
    />
  );
}
