"use client";

import { Hash } from "lucide-react";
import { useRouter } from "next/navigation";
import { useMemo, useState, type FormEvent } from "react";

import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { RoomKind } from "@/gen/axon/chat/v1/chat_pb";
import { describe } from "@/lib/errors";
import { useDebouncedValue } from "@/lib/hooks/use-debounced-value";
import { useCreateRoom, useDirectRoomLookup, useJoinRoom, useRooms } from "@/lib/query/chat";
import { useSearchUsers } from "@/lib/query/people";

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function NewChatDialog({ onDone }: { onDone: () => void }) {
  return (
    <Tabs defaultValue="people">
      <TabsList className="w-full">
        <TabsTrigger value="people" className="flex-1">
          People
        </TabsTrigger>
        <TabsTrigger value="group" className="flex-1">
          Group
        </TabsTrigger>
        <TabsTrigger value="join" className="flex-1">
          Join by ID
        </TabsTrigger>
      </TabsList>

      <TabsContent value="people" className="mt-4">
        <PeopleTab onDone={onDone} />
      </TabsContent>
      <TabsContent value="group" className="mt-4">
        <GroupTab onDone={onDone} />
      </TabsContent>
      <TabsContent value="join" className="mt-4">
        <JoinTab onDone={onDone} />
      </TabsContent>
    </Tabs>
  );
}

function PeopleTab({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const [query, setQuery] = useState("");
  const debouncedQuery = useDebouncedValue(query, 350);
  const trimmed = debouncedQuery.trim();
  const isRoomID = uuidPattern.test(trimmed);

  const search = useSearchUsers(isRoomID ? "" : debouncedQuery);
  const lookup = useDirectRoomLookup();
  const joinRoom = useJoinRoom();
  const rooms = useRooms();

  const roomMatches = useMemo(() => {
    if (!trimmed || isRoomID) return [];
    const q = trimmed.toLowerCase();
    return (rooms.data ?? []).filter(
      (room) => room.kind === RoomKind.OPEN && room.name.toLowerCase().includes(q),
    );
  }, [rooms.data, trimmed, isRoomID]);

  const pick = (profile: PublicProfile) => {
    lookup.mutate(profile.id, {
      onSuccess: (room) => {
        router.push(room ? `/chat/${room.id}` : `/chat/new/${profile.id}`);
        onDone();
      },
    });
  };

  const openRoom = (roomID: string) => {
    router.push(`/chat/${roomID}`);
    onDone();
  };

  const joinByID = () => {
    joinRoom.mutate(trimmed, {
      onSuccess: (id) => {
        router.push(`/chat/${id}`);
        onDone();
      },
    });
  };

  const noMatches =
    trimmed !== "" &&
    !isRoomID &&
    roomMatches.length === 0 &&
    !search.isLoading &&
    (search.data?.length ?? 0) === 0;

  return (
    <div className="space-y-3">
      <div className="space-y-2">
        <Label htmlFor="people-query">Search</Label>
        <Input
          id="people-query"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Email, display name, group, or room ID"
        />
        <p className="text-xs text-muted-foreground">People match by exact email or display name.</p>
      </div>

      {search.isError ? <Alert>{describe(search.error)}</Alert> : null}
      {lookup.isError ? <Alert>{describe(lookup.error)}</Alert> : null}
      {joinRoom.isError ? <Alert>{describe(joinRoom.error)}</Alert> : null}

      <ul className="max-h-72 divide-y divide-border overflow-y-auto">
        {isRoomID ? (
          <li>
            <button
              type="button"
              disabled={joinRoom.isPending}
              onClick={joinByID}
              className="flex w-full items-center gap-3 rounded-md px-2 py-2 text-left text-sm transition-colors hover:bg-muted disabled:opacity-50"
            >
              <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <Hash className="size-4" />
              </span>
              <span className="min-w-0">
                <span className="block font-medium">Open room</span>
                <span className="block truncate text-xs text-muted-foreground">{trimmed}</span>
              </span>
            </button>
          </li>
        ) : null}

        {roomMatches.map((room) => (
          <li key={room.id}>
            <button
              type="button"
              onClick={() => openRoom(room.id)}
              className="flex w-full items-center gap-3 rounded-md px-2 py-2 text-left text-sm transition-colors hover:bg-muted"
            >
              <Avatar
                alt=""
                fallback={room.name.slice(0, 1).toUpperCase()}
                className="size-9"
                sizes="36px"
              />
              <span className="font-medium">{room.name}</span>
            </button>
          </li>
        ))}

        {!isRoomID
          ? search.data?.map((profile) => (
              <li key={profile.id}>
                <button
                  type="button"
                  disabled={lookup.isPending}
                  onClick={() => pick(profile)}
                  className="flex w-full items-center gap-3 rounded-md px-2 py-2 text-left text-sm transition-colors hover:bg-muted disabled:opacity-50"
                >
                  <Avatar
                    src={profile.avatarUrls?.small || profile.avatarUrl}
                    alt=""
                    fallback={(profile.displayName || "?").slice(0, 1).toUpperCase()}
                    className="size-9"
                    sizes="36px"
                  />
                  <span className="font-medium">{profile.displayName || "No display name"}</span>
                  {profile.online ? (
                    <span className="ml-auto size-2 shrink-0 rounded-full bg-green-500" />
                  ) : null}
                </button>
              </li>
            ))
          : null}

        {noMatches ? (
          <li className="px-2 py-6 text-center text-sm text-muted-foreground">
            No match for &quot;{trimmed}&quot;.
          </li>
        ) : null}
      </ul>
    </div>
  );
}

function GroupTab({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const createRoom = useCreateRoom();
  const [name, setName] = useState("");

  const onCreate = (event: FormEvent) => {
    event.preventDefault();
    if (!name.trim()) return;

    createRoom.mutate(name.trim(), {
      onSuccess: (room) => {
        setName("");
        if (room) {
          router.push(`/chat/${room.id}`);
          onDone();
        }
      },
    });
  };

  return (
    <form className="space-y-3" onSubmit={onCreate}>
      <div className="space-y-2">
        <Label htmlFor="new-group-name">Group name</Label>
        <Input
          id="new-group-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="general"
        />
      </div>
      {createRoom.isError ? <Alert>{describe(createRoom.error)}</Alert> : null}
      <Button type="submit" className="w-full" disabled={createRoom.isPending}>
        Create
      </Button>
    </form>
  );
}

function JoinTab({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const joinRoom = useJoinRoom();
  const [roomID, setRoomID] = useState("");

  const onJoin = (event: FormEvent) => {
    event.preventDefault();
    if (!roomID.trim()) return;

    joinRoom.mutate(roomID.trim(), {
      onSuccess: (id) => {
        setRoomID("");
        router.push(`/chat/${id}`);
        onDone();
      },
    });
  };

  return (
    <form className="space-y-3" onSubmit={onJoin}>
      <div className="space-y-2">
        <Label htmlFor="join-room-id">Room ID</Label>
        <Input
          id="join-room-id"
          value={roomID}
          onChange={(event) => setRoomID(event.target.value)}
          placeholder="00000000-0000-0000-0000-000000000000"
        />
      </div>
      {joinRoom.isError ? <Alert>{describe(joinRoom.error)}</Alert> : null}
      <Button type="submit" variant="outline" className="w-full" disabled={joinRoom.isPending}>
        Join
      </Button>
    </form>
  );
}
