"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { describe } from "@/lib/errors";
import { useDebouncedValue } from "@/lib/hooks/use-debounced-value";
import { useDirectRoomLookup } from "@/lib/query/chat";
import { useSearchUsers } from "@/lib/query/people";

export function NewChatDialog({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const [query, setQuery] = useState("");
  const debouncedQuery = useDebouncedValue(query, 350);
  const trimmed = debouncedQuery.trim();

  const search = useSearchUsers(debouncedQuery);
  const lookup = useDirectRoomLookup();

  const pick = (profile: PublicProfile) => {
    lookup.mutate(profile.id, {
      onSuccess: (room) => {
        router.push(room ? `/chat/${room.id}` : `/chat/new/${profile.id}`);
        onDone();
      },
    });
  };

  const noMatches = trimmed !== "" && !search.isLoading && (search.data?.length ?? 0) === 0;

  return (
    <div className="space-y-3">
      <div className="space-y-2">
        <Label htmlFor="people-query">Search</Label>
        <Input
          id="people-query"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Email or display name"
        />
      </div>

      {search.isError ? <Alert>{describe(search.error)}</Alert> : null}
      {lookup.isError ? <Alert>{describe(lookup.error)}</Alert> : null}

      <ScrollArea className="max-h-72">
        <ul className="divide-y divide-border">
          {search.data?.map((profile) => (
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
          ))}

          {noMatches ? (
            <li className="px-2 py-6 text-center text-sm text-muted-foreground">
              No match for &quot;{trimmed}&quot;.
            </li>
          ) : null}
        </ul>
      </ScrollArea>
    </div>
  );
}
