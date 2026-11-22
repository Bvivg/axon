"use client";

import { Image, MessageSquare, Users } from "lucide-react";
import { usePathname } from "next/navigation";

import { CallHistorySidebar } from "@/components/calls/call-history-sidebar";
import { ChatSidebar } from "@/components/chat/chat-sidebar";
import { GamesSidebar } from "@/components/games/catalogue";
import { adminConsoleItem, Section, sectionFor, sectionLabels } from "@/components/nav/items";
import { profileOverview, profilePages } from "@/components/profile/profile-nav";
import { ListColumnHeader } from "@/components/ui/list-column";
import { ListIcon, ListItem } from "@/components/ui/list-item";
import { useAdminAccess } from "@/lib/admin/access";
import { useSession } from "@/lib/auth/session";

export function ContextSidebar() {
  const pathname = usePathname();
  const section = sectionFor(pathname);
  const { user } = useSession();
  const access = useAdminAccess();
  if (!section || !user) return null;

  const links = section === Section.Profile ? [profileOverview, ...profilePages]
    : section === Section.Contacts ? [{ href: "/contacts", label: "All contacts", icon: Users }]
    : section === Section.Media ? [{ href: "/media", label: "Shared media", icon: Image }, { href: "/chat", label: "Browse chats", icon: MessageSquare }]
    : [];

  return (
    <aside className="workspace-context" data-context={section} aria-label={section === Section.Chats ? "Chats" : "Section sidebar"}>
      {section === Section.Chats ? <ChatSidebar /> : section === Section.Calls ? <CallHistorySidebar /> : section === Section.Games ? <GamesSidebar /> : (
        <>
          <ListColumnHeader title={sectionLabels[section]} />
          <nav className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto" aria-label={section === Section.Profile ? "Profile" : "Section"}>
            {links.map(({ href, label, icon }) => (
              <ListItem
                key={href}
                href={href}
                active={pathname === href || (href === profileOverview.href && pathname === "/profile")}
                leading={<ListIcon icon={icon} />}
                title={label}
                className="min-h-11"
              />
            ))}
            {section === Section.Profile && (access.staff || access.admin) ? (
              <ListItem external href={adminConsoleItem.href} leading={<ListIcon icon={adminConsoleItem.icon} />} title={adminConsoleItem.label} className="min-h-11" />
            ) : null}
          </nav>
        </>
      )}
    </aside>
  );
}
