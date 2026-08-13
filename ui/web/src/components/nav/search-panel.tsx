"use client";

import { Search, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";

import type { Section } from "@/components/nav/items";
import { useSession } from "@/lib/auth/session";
import { signInPath } from "@/lib/auth/guards";
import { useRooms } from "@/lib/query/chat";

const placeholders: Record<Section, string> = {
  games: "Enter a game code",
  calls: "Search by nickname or email",
  chats: "Search your rooms",
  profile: "Search settings",
};

export function SearchPanel({ section, onClose }: { section: Section; onClose: () => void }) {
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onClose();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-background/60 px-4 pt-[12vh] backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={placeholders[section]}
        className="w-full max-w-md overflow-hidden rounded-xl border border-border bg-card shadow-2xl"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-center gap-3 border-b border-border px-4 py-3">
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <input
            ref={inputRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={placeholders[section]}
            className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
          />
          <button
            onClick={onClose}
            aria-label="Close search"
            className="shrink-0 text-muted-foreground transition-colors hover:text-foreground"
          >
            <X className="size-4" />
          </button>
        </div>

        <div className="max-h-80 overflow-y-auto p-2">
          {section === "games" ? <GamesSearch query={query} /> : null}
          {section === "calls" ? <CallsSearch query={query} /> : null}
          {section === "chats" ? <ChatsSearch query={query} onNavigate={onClose} /> : null}
          {section === "profile" ? <ProfileSearch query={query} onClose={onClose} /> : null}
        </div>
      </div>
    </div>
  );
}

function Empty({ children }: { children: React.ReactNode }) {
  return <p className="px-3 py-8 text-center text-sm text-muted-foreground">{children}</p>;
}

function GamesSearch({ query }: { query: string }) {
  if (!query.trim()) {
    return <Empty>Type a game&apos;s code to join a session.</Empty>;
  }
  return (
    <Empty>
      Games aren&apos;t wired up yet — code <span className="font-mono text-foreground">{query}</span>{" "}
      would join a session here.
    </Empty>
  );
}

function CallsSearch({ query }: { query: string }) {
  if (!query.trim()) {
    return <Empty>Search recent calls by nickname or email.</Empty>;
  }
  return <Empty>Calling isn&apos;t set up yet — nothing to find for &quot;{query}&quot;.</Empty>;
}

function ChatsSearch({ query, onNavigate }: { query: string; onNavigate: () => void }) {
  const rooms = useRooms();
  const router = useRouter();

  const matches = useMemo(() => {
    const q = query.trim().toLowerCase();
    const all = rooms.data ?? [];
    return q ? all.filter((room) => room.name.toLowerCase().includes(q)) : all;
  }, [rooms.data, query]);

  if (rooms.isLoading) {
    return <Empty>Loading your rooms…</Empty>;
  }

  if (matches.length === 0) {
    return query.trim() ? (
      <Empty>
        No room matches &quot;{query}&quot;. Finding people with no shared room yet is coming soon.
      </Empty>
    ) : (
      <Empty>None yet — open one from Chats.</Empty>
    );
  }

  return (
    <div className="space-y-1">
      {matches.map((room) => (
        <button
          key={room.id}
          onClick={() => {
            router.push(`/chat/${room.id}`);
            onNavigate();
          }}
          className="flex w-full items-center justify-between rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-muted"
        >
          <span className="font-medium">{room.name}</span>
          <span className="text-xs text-muted-foreground">
            {room.memberCount} {room.memberCount === 1 ? "member" : "members"}
          </span>
        </button>
      ))}
    </div>
  );
}

interface SettingEntry {
  label: string;
  hint: string;
  action: "sign-out" | "account";
}

const settings: SettingEntry[] = [
  { label: "Account", hint: "Email, display name and verification status", action: "account" },
  { label: "Sign out", hint: "End this session on this device", action: "sign-out" },
];

function ProfileSearch({ query, onClose }: { query: string; onClose: () => void }) {
  const { signOut } = useSession();
  const router = useRouter();

  const matches = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q
      ? settings.filter(
          (setting) =>
            setting.label.toLowerCase().includes(q) || setting.hint.toLowerCase().includes(q),
        )
      : settings;
  }, [query]);

  if (matches.length === 0) {
    return <Empty>No setting matches &quot;{query}&quot;.</Empty>;
  }

  return (
    <div className="space-y-1">
      {matches.map((setting) => (
        <button
          key={setting.label}
          onClick={() => {
            onClose();
            if (setting.action === "sign-out") {
              void signOut().then(() => router.replace(signInPath));
            } else {
              router.push("/profile");
            }
          }}
          className="flex w-full flex-col items-start rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-muted"
        >
          <span className="font-medium">{setting.label}</span>
          <span className="text-xs text-muted-foreground">{setting.hint}</span>
        </button>
      ))}
    </div>
  );
}
