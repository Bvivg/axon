"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { describe } from "@/lib/errors";
import { useCreateRoom, useJoinRoom, useRooms } from "@/lib/query/chat";

export default function ChatLobbyPage() {
  const { status } = useSession();
  const router = useRouter();

  useRequireSession();

  const rooms = useRooms();
  const createRoom = useCreateRoom();
  const joinRoom = useJoinRoom();

  const [name, setName] = useState("");
  const [roomID, setRoomID] = useState("");

  if (status !== "authenticated") {
    return (
      <main className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </main>
    );
  }

  const onCreate = (event: FormEvent) => {
    event.preventDefault();
    if (!name.trim()) return;

    createRoom.mutate(name.trim(), {
      onSuccess: (room) => {
        setName("");
        if (room) router.push(`/chat/${room.id}`);
      },
    });
  };

  const onJoin = (event: FormEvent) => {
    event.preventDefault();
    if (!roomID.trim()) return;

    joinRoom.mutate(roomID.trim(), {
      onSuccess: (id) => {
        setRoomID("");
        router.push(`/chat/${id}`);
      },
    });
  };

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-2xl flex-col gap-6 px-6 py-12">
      <header className="flex items-baseline justify-between">
        <h1 className="text-2xl font-semibold">Chat</h1>
        <Link className="text-sm underline underline-offset-4" href="/profile">
          Profile
        </Link>
      </header>

      <Card>
        <CardHeader>
          <CardTitle>Your rooms</CardTitle>
        </CardHeader>

        <CardContent>
          {rooms.isError ? <Alert>{describe(rooms.error)}</Alert> : null}

          <ul className="divide-y divide-border">
            {rooms.data?.map((room) => (
              <li key={room.id}>
                <Link
                  className="flex items-center justify-between py-3 text-sm hover:text-foreground"
                  href={`/chat/${room.id}`}
                >
                  <span className="font-medium">{room.name}</span>
                  <span className="text-muted-foreground">
                    {room.memberCount} {room.memberCount === 1 ? "member" : "members"}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Open a room</CardTitle>
        </CardHeader>
        <CardContent>
          <form className="flex items-end gap-3" onSubmit={onCreate}>
            <div className="flex-1 space-y-2">
              <Label htmlFor="room-name">Name</Label>
              <Input
                id="room-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="general"
              />
            </div>
            <Button type="submit" disabled={createRoom.isPending}>
              Create
            </Button>
          </form>
          {createRoom.isError ? (
            <Alert className="mt-3">{describe(createRoom.error)}</Alert>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Join one</CardTitle>
        </CardHeader>
        <CardContent>
          <form className="flex items-end gap-3" onSubmit={onJoin}>
            <div className="flex-1 space-y-2">
              <Label htmlFor="room-id">Room ID</Label>
              <Input
                id="room-id"
                value={roomID}
                onChange={(event) => setRoomID(event.target.value)}
                placeholder="00000000-0000-0000-0000-000000000000"
              />
            </div>
            <Button type="submit" variant="outline" disabled={joinRoom.isPending}>
              Join
            </Button>
          </form>
          {joinRoom.isError ? (
            <Alert className="mt-3">{describe(joinRoom.error)}</Alert>
          ) : null}
        </CardContent>
      </Card>
    </main>
  );
}
