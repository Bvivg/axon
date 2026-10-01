"use client";

import { Gamepad2, MessageCircle, Phone, Search, User, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { isConversation, Section, sectionFor } from "@/components/nav/items";
import { setPaletteOpen } from "@/components/nav/palette-store";
import { Avatar } from "@/components/ui/avatar";
import { useSession } from "@/lib/auth/session";
import { cn } from "@/lib/utils";

interface Tab {
  section: Section;
  href: string;
  label: string;
  icon: LucideIcon;
}

const tabs: Tab[] = [
  { section: Section.Lobby, href: "/", label: "Games", icon: Gamepad2 },
  { section: Section.Calls, href: "/calls", label: "Calls", icon: Phone },
  { section: Section.Chats, href: "/chat", label: "Chats", icon: MessageCircle },
  { section: Section.Profile, href: "/profile", label: "Profile", icon: User },
];

export function Tabbar() {
  const pathname = usePathname();
  const section = sectionFor(pathname);
  const { user } = useSession();
  const avatarSrc = user?.avatarUrls?.small || user?.avatarUrl;

  return (
    <div
      className={cn(
        "pointer-events-none fixed inset-x-0 bottom-0 z-40 flex items-center justify-center gap-3 px-4 pb-[max(1rem,env(safe-area-inset-bottom))] lg:hidden",
        isConversation(pathname) && "max-md:hidden",
      )}
    >
      <nav
        aria-label="Primary"
        className="pointer-events-auto flex flex-1 items-center justify-between rounded-full border border-border bg-card/95 px-1 py-1.5 shadow-lg backdrop-blur"
      >
        {tabs.map((tab) => {
          const Icon = tab.icon;
          const active = tab.section === section;
          const showAvatar = tab.section === Section.Profile && !!avatarSrc;
          return (
            <Link
              key={tab.href}
              href={tab.href}
              aria-current={active ? "page" : undefined}
              className={cn(
                "flex flex-1 flex-col items-center gap-0.5 rounded-full py-1.5 text-[10px] transition-colors",
                active ? "text-signal" : "text-muted-foreground",
              )}
            >
              {showAvatar ? (
                <Avatar src={avatarSrc} alt="" fallback="" sizes="20px" className="size-5" />
              ) : (
                <Icon className="size-5" />
              )}
              {tab.label}
            </Link>
          );
        })}
      </nav>

      <button
        type="button"
        onClick={() => setPaletteOpen(true)}
        aria-label="Search"
        className="pointer-events-auto flex size-14 shrink-0 items-center justify-center rounded-full bg-signal text-signal-foreground shadow-lg transition-transform active:scale-95"
      >
        <Search className="size-5" />
      </button>
    </div>
  );
}
