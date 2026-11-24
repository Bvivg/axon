"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { ChevronRight } from "lucide-react";

import { useRoomDirectory } from "@/components/chat/use-room-directory";
import { BrandMark } from "@/components/nav/brand";
import { navItems, Section, sectionFor } from "@/components/nav/items";
import { Avatar } from "@/components/ui/avatar";
import { useSession } from "@/lib/auth/session";
import { initials, personName } from "@/lib/format";

export function AppSidebar() {
  const section = sectionFor(usePathname());
  const { unread } = useRoomDirectory();
  const { user } = useSession();
  const current = section === Section.Contacts || section === Section.Media ? Section.Chats : section;

  return (
    <aside className="workspace-rail call-app-tabbar">
      <Link href="/chat" className="workspace-brand" aria-label="Axon home">
        <BrandMark className="size-8 rounded-[10px]" />
        <span className="workspace-brand-copy"><strong>AXON</strong></span>
      </Link>
      <nav aria-label="Primary">
        {navItems.map(({ href, section, icon: Icon, label }) => (
          <Link key={href} href={href} title={label} aria-label={label}
            aria-current={current === section ? "page" : undefined}
            aria-describedby={section === Section.Chats && unread > 0 ? "workspace-chat-unread" : undefined}
            className="workspace-rail-link">
            <span className="workspace-nav-icon">
              <Icon aria-hidden size={22} strokeWidth={1.8} />
            {section === Section.Chats && unread > 0 ? (
              <><span aria-hidden className="workspace-unread-dot" /><span id="workspace-chat-unread" className="sr-only">{unread} unread messages</span></>
            ) : null}
            </span>
            <span className="workspace-nav-label">{label}</span>
          </Link>
        ))}
      </nav>
      <div className="workspace-rail-bottom">
        {user ? <Link href="/profile?view=account" className="workspace-user-link" aria-label="My account">
          <Avatar self src={user.avatarUrls?.small || user.avatarUrl} alt="" fallback={initials(personName(user))} sizes="38px" className="size-[38px] rounded-xl text-xs" />
          <span><strong>{personName(user)}</strong><small>{user.username ? `@${user.username}` : user.email}</small></span><ChevronRight aria-hidden size={14} className="shrink-0" />
        </Link> : null}
      </div>
    </aside>
  );
}
