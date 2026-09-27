"use client";

import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import {
  Hash,
  IdCard,
  LogOut,
  MonitorSmartphone,
  Palette,
  Search,
  SquarePen,
  SunMoon,
  type LucideIcon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState, type KeyboardEvent, type ReactNode } from "react";

import { openComposer } from "@/components/chat/composer-store";
import { useRoomDirectory, type RoomEntry } from "@/components/chat/use-room-directory";
import { navItems } from "@/components/nav/items";
import { setPaletteOpen, usePaletteOpen } from "@/components/nav/palette-store";
import { Avatar } from "@/components/ui/avatar";
import { Kbd, KbdCombo } from "@/components/ui/kbd";
import { ScrollArea } from "@/components/ui/scroll-area";
import { LoadingDots, PresenceDot } from "@/components/ui/status";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { useSession } from "@/lib/auth/session";
import { fromTimestamp, initials, lastSeen } from "@/lib/format";
import { useDebouncedValue } from "@/lib/hooks/use-debounced-value";
import { useModKey } from "@/lib/hooks/use-is-mac";
import { useDirectRoomLookup } from "@/lib/query/chat";
import { useSearchUsers } from "@/lib/query/people";
import { Theme, toggleTheme, useResolvedTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

interface Command {
  id: string;
  group: string;
  label: string;
  icon: ReactNode;
  hint?: ReactNode;
  keys?: string[];
  keywords?: string;
  run: () => void;
}

const suggestedLimit = 3;
const roomLimit = 6;

export function CommandPalette() {
  const open = usePaletteOpen();

  return (
    <BaseDialog.Root open={open} onOpenChange={setPaletteOpen}>
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className="fixed inset-0 z-50 bg-scrim transition-opacity duration-200 ease-signal data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
        <BaseDialog.Popup
          aria-label="Command palette"
          className={cn(
            "fixed top-3 left-1/2 z-50 flex max-h-[calc(100dvh-1.5rem)] w-[min(calc(100vw-1.5rem),600px)] -translate-x-1/2 flex-col overflow-hidden rounded-xl border border-border bg-popover text-popover-foreground shadow-overlay outline-none md:top-[14vh] md:max-h-[72vh]",
            "transition-[opacity,translate] duration-200 ease-signal data-[ending-style]:opacity-0 data-[starting-style]:translate-y-1 data-[starting-style]:opacity-0",
          )}
        >
          <PaletteBody />
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  );
}

function PaletteBody() {
  const router = useRouter();
  const mod = useModKey();
  const { user, signOut } = useSession();
  const { entries } = useRoomDirectory();
  const resolved = useResolvedTheme();
  const lookup = useDirectRoomLookup();

  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const needle = query.trim().toLowerCase();
  const debounced = useDebouncedValue(needle, 250);
  const people = useSearchUsers(debounced);

  const commands = useMemo<Command[]>(() => {
    const go = (href: string) => () => {
      router.push(href);
      setPaletteOpen(false);
    };

    const roomCommand = (group: string) => (entry: RoomEntry): Command => ({
      id: `room-${entry.room.id}`,
      group,
      label: entry.direct ? entry.label : `#${entry.label}`,
      icon: entry.direct ? (
        <PersonMark name={entry.label} src={entry.avatarSrc} online={!!entry.peer?.online} />
      ) : (
        <Hash aria-hidden className="size-4 text-muted-foreground" strokeWidth={1.5} />
      ),
      hint: entry.unread > 0 ? `${entry.unread} unread` : undefined,
      run: go(`/chat/${entry.room.id}`),
    });

    const personCommand = (profile: PublicProfile): Command => ({
      id: `person-${profile.id}`,
      group: "People",
      label: profile.displayName || "No display name",
      icon: (
        <PersonMark
          name={profile.displayName || "?"}
          src={profile.avatarUrls?.small || profile.avatarUrl}
          online={profile.online}
        />
      ),
      hint: profile.online ? "online" : lastSeen(fromTimestamp(profile.lastSeenAt)),
      run: () => {
        lookup.mutate(profile.id, {
          onSuccess: (room) => {
            router.push(room ? `/chat/${room.id}` : `/chat/new/${profile.id}`);
            setPaletteOpen(false);
          },
        });
      },
    });

    const icon = (Icon: LucideIcon) => (
      <Icon aria-hidden className="size-4 text-muted-foreground" strokeWidth={1.5} />
    );

    const navigation: Command[] = [
      ...navItems.map((item) => ({
        id: `go-${item.href}`,
        group: "Go to",
        label: item.label,
        icon: icon(item.icon),
        keys: item.keys,
        run: go(item.href),
      })),
      { id: "go-profile", group: "Go to", label: "Personal info", icon: icon(IdCard), keywords: "profile account", run: go("/profile/my") },
      { id: "go-sessions", group: "Go to", label: "Sessions", icon: icon(MonitorSmartphone), keywords: "devices", run: go("/profile/sessions") },
      { id: "go-appearance", group: "Go to", label: "Appearance", icon: icon(Palette), keywords: "theme", run: go("/profile/appearance") },
    ];

    const other = resolved === Theme.Dark ? Theme.Light : Theme.Dark;
    const compose = () => {
      setPaletteOpen(false);
      openComposer();
    };
    const actions: Command[] = [
      { id: "new-chat", group: "Actions", label: "New chat", icon: icon(SquarePen), keywords: "message direct person contact", run: compose },
      {
        id: "toggle-theme",
        group: "Preferences",
        label: "Toggle theme",
        icon: icon(SunMoon),
        hint: `${resolved} → ${other}`,
        keys: [mod, "⇧", "L"],
        keywords: "dark light",
        run: () => toggleTheme(),
      },
      { id: "sign-out", group: "Preferences", label: "Sign out", icon: icon(LogOut), keywords: "log out", run: () => {
        setPaletteOpen(false);
        void signOut();
      } },
    ];

    const matches = (command: Command) =>
      !needle || `${command.label} ${command.keywords ?? ""}`.toLowerCase().includes(needle);

    if (!needle) {
      const suggested = entries
        .filter((entry) => entry.unread > 0)
        .slice(0, suggestedLimit)
        .map(roomCommand("Suggested"));
      return [...suggested, ...navigation, ...actions];
    }

    const rooms = entries
      .filter((entry) => entry.label.toLowerCase().includes(needle.replace(/^#/, "")))
      .slice(0, roomLimit)
      .map(roomCommand("Chats"));
    const found = (people.data ?? [])
      .filter((profile) => profile.id !== user?.id)
      .map(personCommand);

    return [...rooms, ...found, ...navigation.filter(matches), ...actions.filter(matches)];
  }, [entries, needle, people.data, user?.id, resolved, mod, router, signOut, lookup]);

  const current = Math.min(active, Math.max(commands.length - 1, 0));
  const activeID = commands[current] ? `palette-${commands[current].id}` : undefined;

  useEffect(() => {
    if (activeID) {
      document.getElementById(activeID)?.scrollIntoView({ block: "nearest" });
    }
  }, [activeID]);

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    const last = commands.length - 1;
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        setActive(current >= last ? 0 : current + 1);
        break;
      case "ArrowUp":
        event.preventDefault();
        setActive(current <= 0 ? last : current - 1);
        break;
      case "Home":
        event.preventDefault();
        setActive(0);
        break;
      case "End":
        event.preventDefault();
        setActive(last);
        break;
      case "Enter":
        event.preventDefault();
        commands[current]?.run();
        break;
    }
  };

  const groups = groupBy(commands);
  const searching = needle !== "" && (people.isFetching || debounced !== needle);

  return (
    <>
      <div className="flex h-[52px] shrink-0 items-center gap-2.5 border-b border-border px-3.5">
        <Search aria-hidden className="size-4 shrink-0 text-muted-foreground" strokeWidth={1.5} />
        <input
          role="combobox"
          aria-expanded
          aria-controls="palette-list"
          aria-autocomplete="list"
          aria-activedescendant={activeID}
          aria-label="Search chats, people and commands"
          placeholder="Type a command or search chats and people…"
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            setActive(0);
          }}
          onKeyDown={onKeyDown}
          className="min-w-0 flex-1 bg-transparent text-md outline-none placeholder:text-muted-foreground focus-visible:outline-none"
        />
        {searching ? <LoadingDots className="text-muted-foreground" label="Searching" /> : null}
        <BaseDialog.Close
          aria-label="Close"
          className="inline-flex h-[22px] items-center rounded-sm border border-border bg-muted px-1.5 font-mono text-2xs text-muted-foreground"
        >
          esc
        </BaseDialog.Close>
      </div>

      <ScrollArea className="flex-1">
        <div id="palette-list" role="listbox" aria-label="Results" className="p-1.5">
          {commands.length === 0 ? (
            <p className="px-2.5 py-8 text-center text-sm text-muted-foreground">
              {searching ? "Searching…" : `Nothing matches “${query.trim()}”`}
            </p>
          ) : (
            groups.map(([group, items]) => (
              <div key={group} role="group" aria-labelledby={`palette-group-${group}`} className="flex flex-col gap-0.5">
                <div id={`palette-group-${group}`} className="eyebrow px-2.5 pt-2.5 pb-1">
                  {group}
                </div>
                {items.map((command) => {
                  const index = commands.indexOf(command);
                  const selected = index === current;
                  return (
                    <div
                      key={command.id}
                      id={`palette-${command.id}`}
                      role="option"
                      aria-selected={selected}
                      onMouseMove={() => setActive(index)}
                      onClick={command.run}
                      className={cn(
                        "flex h-9 shrink-0 cursor-pointer items-center gap-3 rounded-md px-2.5 text-sm",
                        selected && "bg-accent",
                      )}
                    >
                      <span className="flex w-4 justify-center">{command.icon}</span>
                      <span className="min-w-0 flex-1 truncate">{command.label}</span>
                      {command.hint ? (
                        <span className="shrink-0 font-mono text-2xs text-muted-foreground">{command.hint}</span>
                      ) : null}
                      {command.keys ? <KbdCombo aria-hidden keys={command.keys} /> : null}
                      {selected ? <Kbd aria-hidden>↵</Kbd> : null}
                    </div>
                  );
                })}
              </div>
            ))
          )}
        </div>
      </ScrollArea>

      <div className="hidden h-10 shrink-0 items-center gap-4 border-t border-border px-3.5 text-xs text-muted-foreground md:flex">
        <span className="flex items-center gap-1.5">
          <KbdCombo aria-hidden keys={["↑", "↓"]} />
          navigate
        </span>
        <span className="flex items-center gap-1.5">
          <Kbd aria-hidden>↵</Kbd>
          open
        </span>
        <span className="flex items-center gap-1.5">
          <Kbd aria-hidden>esc</Kbd>
          close
        </span>
        <span className="flex-1" />
        <span className="font-mono text-2xs tabular-nums">
          {commands.length} {commands.length === 1 ? "result" : "results"}
        </span>
      </div>
    </>
  );
}

function PersonMark({ name, src, online }: { name: string; src?: string; online: boolean }) {
  return (
    <Avatar src={src} alt="" fallback={initials(name)} sizes="16px" className="size-4 text-[8px]">
      <PresenceDot online={online} className="size-[7px] ring-popover" />
    </Avatar>
  );
}

function groupBy(commands: Command[]): [string, Command[]][] {
  const groups = new Map<string, Command[]>();
  for (const command of commands) {
    const list = groups.get(command.group) ?? [];
    list.push(command);
    groups.set(command.group, list);
  }
  return [...groups.entries()];
}
