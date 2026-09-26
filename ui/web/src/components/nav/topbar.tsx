"use client";

import { Moon, Search, Sun } from "lucide-react";
import { usePathname } from "next/navigation";

import { useRoomDirectory } from "@/components/chat/use-room-directory";
import { Section, sectionFor, sectionLabels } from "@/components/nav/items";
import { setPaletteOpen } from "@/components/nav/palette-store";
import { Button } from "@/components/ui/button";
import { KbdCombo } from "@/components/ui/kbd";
import { Spinner } from "@/components/ui/status";
import { useModKey } from "@/lib/hooks/use-is-mac";
import { Theme, toggleTheme, useResolvedTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { ConnectionStatus, useConnectionStatus } from "@/lib/ws/connection";

const profilePages: Record<string, string> = {
  "/profile/my": "Personal info",
  "/profile/sessions": "Sessions",
  "/profile/appearance": "Appearance",
};

function usePageLabel(pathname: string, section: Section | null): string | null {
  const { entries } = useRoomDirectory();

  if (section === Section.Chats) {
    if (pathname.startsWith("/chat/new/")) {
      return "New message";
    }
    const roomID = pathname.split("/")[2];
    const entry = roomID ? entries.find((candidate) => candidate.room.id === roomID) : undefined;
    if (!entry) {
      return null;
    }
    return entry.direct ? entry.label : `#${entry.label}`;
  }
  if (section === Section.Profile) {
    return profilePages[pathname] ?? null;
  }
  return null;
}

export function Topbar() {
  const pathname = usePathname();
  const section = sectionFor(pathname);
  const page = usePageLabel(pathname, section);

  return (
    <header className="hidden h-[52px] shrink-0 items-center gap-3 border-b border-border bg-background pr-4 pl-5 text-sm md:flex">
      <nav aria-label="Breadcrumb" className="flex min-w-0 flex-1 items-center gap-2">
        {section ? (
          <span className={page ? "text-muted-foreground" : "font-medium"}>{sectionLabels[section]}</span>
        ) : null}
        {page ? (
          <>
            <span aria-hidden className="text-muted-foreground">
              /
            </span>
            <span aria-current="page" className="truncate font-medium">
              {page}
            </span>
          </>
        ) : null}
      </nav>

      <PaletteTrigger />
      <ConnectionPill />
      <ThemeToggle />
    </header>
  );
}

function PaletteTrigger() {
  const mod = useModKey();
  return (
    <button
      type="button"
      onClick={() => setPaletteOpen(true)}
      className="flex h-8 w-56 items-center gap-2 rounded-md border border-border bg-card pr-1.5 pl-2.5 text-left text-muted-foreground transition-colors duration-[120ms] ease-signal hover:bg-accent lg:w-70"
    >
      <Search aria-hidden className="size-4 shrink-0" strokeWidth={1.5} />
      <span className="flex-1 truncate">Search or jump to…</span>
      <KbdCombo aria-hidden keys={[mod, "K"]} />
    </button>
  );
}

export function ConnectionPill({ className }: { className?: string }) {
  const status = useConnectionStatus();

  return (
    <div
      role="status"
      className={cn(
        "flex h-7 shrink-0 items-center gap-2 rounded-full border px-2.5 text-xs",
        status === ConnectionStatus.Connected && "border-border",
        status === ConnectionStatus.Connecting && "border-border text-muted-foreground",
        status === ConnectionStatus.Reconnecting && "border-warning/55 bg-warning/12",
        status === ConnectionStatus.Offline && "border-border bg-muted text-muted-foreground",
        className,
      )}
    >
      {status === ConnectionStatus.Connected ? (
        <span aria-hidden className="size-[7px] rounded-full bg-success" />
      ) : status === ConnectionStatus.Offline ? (
        <span aria-hidden className="size-[7px] rounded-full border-[1.5px] border-muted-foreground" />
      ) : (
        <Spinner className={cn("size-3", status === ConnectionStatus.Reconnecting && "text-warning")} />
      )}
      {connectionLabels[status]}
    </div>
  );
}

const connectionLabels: Record<ConnectionStatus, string> = {
  [ConnectionStatus.Connecting]: "Connecting…",
  [ConnectionStatus.Connected]: "Connected",
  [ConnectionStatus.Reconnecting]: "Reconnecting…",
  [ConnectionStatus.Offline]: "Offline",
};

export function ThemeToggle() {
  const resolved = useResolvedTheme();
  const dark = resolved === Theme.Dark;

  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={dark ? "Switch to light theme" : "Switch to dark theme"}
      onClick={toggleTheme}
    >
      {dark ? <Sun strokeWidth={1.5} /> : <Moon strokeWidth={1.5} />}
    </Button>
  );
}
