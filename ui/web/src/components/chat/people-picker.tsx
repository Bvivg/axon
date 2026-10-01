"use client";

import { Check, X } from "lucide-react";
import { useState, type ReactNode } from "react";

import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { describe } from "@/lib/errors";
import { useDebouncedValue } from "@/lib/hooks/use-debounced-value";
import { useSearchUsers } from "@/lib/query/people";
import { cn } from "@/lib/utils";

export function PeoplePicker({
  selected,
  onChange,
  exclude,
  selfID,
}: {
  selected: PublicProfile[];
  onChange: (people: PublicProfile[]) => void;
  exclude?: ReadonlySet<string>;
  selfID?: string;
}) {
  const chosen = new Set(selected.map((p) => p.id));

  const toggle = (profile: PublicProfile) => {
    onChange(chosen.has(profile.id) ? selected.filter((p) => p.id !== profile.id) : [...selected, profile]);
  };

  return (
    <div className="space-y-3">
      {selected.length > 0 ? (
        <ul aria-label="Selected people" className="flex flex-wrap gap-1.5">
          {selected.map((profile) => (
            <li
              key={profile.id}
              className="flex h-7 items-center gap-1.5 rounded-full bg-signal/10 pr-1 pl-1 text-xs font-medium"
            >
              <Avatar
                src={profile.avatarUrls?.small || profile.avatarUrl}
                alt=""
                fallback={(profile.displayName || "?").slice(0, 1).toUpperCase()}
                className="size-5 text-[10px]"
                sizes="20px"
              />
              {profile.displayName || "No display name"}
              <button
                type="button"
                onClick={() => toggle(profile)}
                aria-label={`Remove ${profile.displayName || "person"}`}
                className="flex size-5 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                <X aria-hidden className="size-3" />
              </button>
            </li>
          ))}
        </ul>
      ) : null}

      <UserSearch
        id="people-picker-query"
        label="Add people"
        hidden={(profile) => profile.id === selfID || !!exclude?.has(profile.id)}
        pressed={(profile) => chosen.has(profile.id)}
        onPick={toggle}
        className="max-h-56"
        trailing={(profile) => (
          <span
            aria-hidden
            className={cn(
              "ml-auto flex size-5 shrink-0 items-center justify-center rounded-full border",
              chosen.has(profile.id) ? "border-signal bg-signal text-signal-foreground" : "border-border",
            )}
          >
            {chosen.has(profile.id) ? <Check className="size-3.5" /> : null}
          </span>
        )}
      />
    </div>
  );
}

export function UserSearch({
  id,
  label,
  hidden,
  pressed,
  disabled,
  notice,
  trailing,
  onPick,
  className,
}: {
  id: string;
  label: string;
  hidden?: (profile: PublicProfile) => boolean;
  pressed?: (profile: PublicProfile) => boolean;
  disabled?: boolean;
  notice?: ReactNode;
  trailing: (profile: PublicProfile) => ReactNode;
  onPick: (profile: PublicProfile) => void;
  className?: string;
}) {
  const [query, setQuery] = useState("");
  const debouncedQuery = useDebouncedValue(query, 350);
  const trimmed = debouncedQuery.trim();
  const search = useSearchUsers(debouncedQuery);

  const results = (search.data ?? []).filter((profile) => !hidden?.(profile));
  const noMatches = trimmed !== "" && !search.isLoading && results.length === 0;

  return (
    <>
      <div className="space-y-2">
        <Label htmlFor={id}>{label}</Label>
        <Input
          id={id}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Email or display name"
        />
      </div>

      {search.isError ? <Alert>{describe(search.error)}</Alert> : null}
      {notice}

      <ScrollArea className={className}>
        <ul className="divide-y divide-border">
          {results.map((profile) => (
            <li key={profile.id}>
              <button
                type="button"
                aria-pressed={pressed?.(profile)}
                disabled={disabled}
                onClick={() => onPick(profile)}
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
                {trailing(profile)}
              </button>
            </li>
          ))}

          {noMatches ? (
            <li className="px-2 py-6 text-center text-sm text-muted-foreground">No match for &quot;{trimmed}&quot;.</li>
          ) : null}
        </ul>
      </ScrollArea>
    </>
  );
}
