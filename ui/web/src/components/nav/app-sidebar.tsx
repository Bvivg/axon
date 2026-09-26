"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { useRoomTyping } from "@/components/chat/chat-activity";
import { useRoomDirectory, type RoomEntry } from "@/components/chat/use-room-directory";
import { AccountMenu } from "@/components/nav/account-menu";
import { BrandMark } from "@/components/nav/brand";
import { navItems, sectionFor, type NavItem } from "@/components/nav/items";
import { Avatar } from "@/components/ui/avatar";
import { CountBadge } from "@/components/ui/badge";
import { KbdCombo } from "@/components/ui/kbd";
import { PresenceDot } from "@/components/ui/status";
import { initials } from "@/lib/format";
import { cn } from "@/lib/utils";

const unreadShown = 6;

const rowClass =
  "flex h-8 items-center gap-2.5 rounded-md px-2 transition-colors duration-[120ms] ease-signal hover:bg-accent";

export function AppSidebar() {
  const pathname = usePathname();
  const active = sectionFor(pathname);
  const { entries, unread } = useRoomDirectory();
  const typing = useRoomTyping();

  const unreadRooms = entries.filter((entry) => entry.unread > 0).slice(0, unreadShown);

  return (
    <aside className="sticky top-0 hidden h-dvh w-62 shrink-0 flex-col gap-[18px] border-r border-border bg-sidebar px-2 py-2.5 text-sm lg:flex">
      <Link href="/" className={cn(rowClass, "h-9 gap-2.5")}>
        <BrandMark />
        <span className="font-semibold">Axon</span>
      </Link>

      <nav aria-label="Primary" className="flex flex-col gap-px">
        {navItems.map((item) => (
          <SidebarLink
            key={item.href}
            item={item}
            active={item.section === active}
            count={item.href === "/chat" ? unread : 0}
          />
        ))}
      </nav>

      {unreadRooms.length > 0 ? (
        <section aria-labelledby="sidebar-unread" className="flex flex-col gap-px">
          <h2 id="sidebar-unread" className="eyebrow px-2 pb-1.5">
            Unread
          </h2>
          {unreadRooms.map((entry) => (
            <UnreadRow
              key={entry.room.id}
              entry={entry}
              current={pathname === `/chat/${entry.room.id}`}
              typing={(typing.get(entry.room.id)?.size ?? 0) > 0}
            />
          ))}
        </section>
      ) : null}

      <div className="flex-1" />

      <AccountMenu />
    </aside>
  );
}

function SidebarLink({ item, active, count }: { item: NavItem; active: boolean; count: number }) {
  const Icon = item.icon;
  return (
    <Link
      href={item.href}
      aria-current={active ? "page" : undefined}
      className={cn(
        rowClass,
        active ? "bg-accent font-medium text-foreground" : "text-muted-foreground",
      )}
    >
      <Icon aria-hidden className="size-4 shrink-0" strokeWidth={1.5} />
      <span className="flex-1">{item.label}</span>
      {count > 0 ? (
        <CountBadge count={count} />
      ) : item.keys ? (
        <KbdCombo aria-hidden keys={item.keys} className="gap-0.5" />
      ) : null}
    </Link>
  );
}

function UnreadRow({
  entry,
  current,
  typing,
}: {
  entry: RoomEntry;
  current: boolean;
  typing: boolean;
}) {
  return (
    <Link
      href={`/chat/${entry.room.id}`}
      aria-current={current ? "page" : undefined}
      className={cn(rowClass, "font-medium text-foreground", current && "bg-accent")}
    >
      {entry.direct ? (
        <Avatar
          src={entry.avatarSrc}
          alt=""
          fallback={initials(entry.label)}
          sizes="16px"
          className="size-4 text-[8px]"
        >
          <PresenceDot online={!!entry.peer?.online} className="size-[7px] ring-sidebar" />
        </Avatar>
      ) : (
        <span aria-hidden className="w-4 text-center font-mono text-muted-foreground">
          #
        </span>
      )}
      <span className="min-w-0 flex-1 truncate">{entry.label}</span>
      {typing ? (
        <span className="font-mono text-2xs text-signal">typing…</span>
      ) : (
        <span className="font-mono text-2xs text-muted-foreground tabular-nums">{entry.unread}</span>
      )}
    </Link>
  );
}
