"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import { PeoplePicker, UserSearch } from "@/components/chat/people-picker";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { useSession } from "@/lib/auth/session";
import { describe } from "@/lib/errors";
import { useCreateGroup, useDirectRoomLookup } from "@/lib/query/chat";

enum NewChatTab {
  Direct = "direct",
  Group = "group",
}

export function NewChatDialog({ onDone }: { onDone: () => void }) {
  return (
    <Tabs defaultValue={NewChatTab.Direct} className="space-y-4">
      <TabsList className="w-full">
        <TabsTrigger value={NewChatTab.Direct} className="flex-1">
          Message
        </TabsTrigger>
        <TabsTrigger value={NewChatTab.Group} className="flex-1">
          New group
        </TabsTrigger>
      </TabsList>
      <TabsContent value={NewChatTab.Direct}>
        <DirectChatPicker onDone={onDone} />
      </TabsContent>
      <TabsContent value={NewChatTab.Group}>
        <NewGroupForm onDone={onDone} />
      </TabsContent>
    </Tabs>
  );
}

function NewGroupForm({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const { user } = useSession();
  const [name, setName] = useState("");
  const [people, setPeople] = useState<PublicProfile[]>([]);
  const create = useCreateGroup();

  const canCreate = name.trim() !== "" && people.length > 0 && !create.isPending;

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (!canCreate) return;
    create.mutate(
      { name: name.trim(), memberIDs: people.map((p) => p.id) },
      {
        onSuccess: (room) => {
          if (room) router.push(`/chat/${room.id}`);
          onDone();
        },
      },
    );
  };

  return (
    <form className="space-y-4" onSubmit={onSubmit}>
      <div className="space-y-2">
        <Label htmlFor="group-name">Group name</Label>
        <Input id="group-name" value={name} maxLength={120} onChange={(event) => setName(event.target.value)} />
      </div>
      <PeoplePicker selected={people} onChange={setPeople} selfID={user?.id} />
      {create.isError ? <Alert>{describe(create.error)}</Alert> : null}
      <Button type="submit" variant="signal" className="w-full" disabled={!canCreate} aria-busy={create.isPending}>
        Create group
      </Button>
    </form>
  );
}

function DirectChatPicker({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const lookup = useDirectRoomLookup();

  const pick = (profile: PublicProfile) => {
    lookup.mutate(profile.id, {
      onSuccess: (room) => {
        router.push(room ? `/chat/${room.id}` : `/chat/new/${profile.id}`);
        onDone();
      },
    });
  };

  return (
    <div className="space-y-3">
      <UserSearch
        id="people-query"
        label="Search"
        disabled={lookup.isPending}
        notice={lookup.isError ? <Alert>{describe(lookup.error)}</Alert> : null}
        onPick={pick}
        className="max-h-72"
        trailing={(profile) =>
          profile.online ? <span className="ml-auto size-2 shrink-0 rounded-full bg-green-500" /> : null
        }
      />
    </div>
  );
}
