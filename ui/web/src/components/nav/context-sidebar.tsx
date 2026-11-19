"use client";

import { Image, MessageSquare, Users } from "lucide-react";
import { usePathname } from "next/navigation";

import { CallHistorySidebar } from "@/components/calls/call-history-sidebar";
import { ChatSidebar } from "@/components/chat/chat-sidebar";
import { GamesSidebar } from "@/components/games/catalogue";
import { adminNavItems, Section, sectionFor, sectionLabels } from "@/components/nav/items";
import { profileOverview, profilePages } from "@/components/profile/profile-nav";
import { ListColumnHeader } from "@/components/ui/list-column";
import { ListIcon, ListItem } from "@/components/ui/list-item";
import { useSession } from "@/lib/auth/session";

export function ContextSidebar() {
  const pathname = usePathname();
  const section = sectionFor(pathname);
  const { user } = useSession();
  if (!section || !user) return null;

  const admin = user.roles.includes("admin");
  const adminSection = section === Section.Admin || section === Section.Gateway || section === Section.ServiceMap;
  const links = adminSection
    ? admin ? adminNavItems.map((entry) => entry.section === Section.Admin ? { ...entry, label: "Users and roles" } : entry) : []
    : section === Section.Profile ? [profileOverview, ...profilePages, ...(admin ? [adminNavItems[0]] : [])]
    : section === Section.Contacts ? [{ href: "/contacts", label: "All contacts", icon: Users }]
    : section === Section.Media ? [{ href: "/media", label: "Shared media", icon: Image }, { href: "/chat", label: "Browse chats", icon: MessageSquare }]
    : [];

  return (
    <aside className="workspace-context" data-context={section} aria-label={section === Section.Chats ? "Chats" : "Section sidebar"}>
      {section === Section.Chats ? <ChatSidebar /> : section === Section.Calls ? <CallHistorySidebar /> : section === Section.Games ? <GamesSidebar /> : (
        <>
          <ListColumnHeader title={adminSection ? "Administration" : sectionLabels[section]} />
          <nav className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto" aria-label={adminSection ? "Administration" : section === Section.Profile ? "Profile" : "Section"}>
            {links.map(({ href, label, icon }) => (
              <ListItem
                key={href}
                href={href}
                active={pathname === href || (href === profileOverview.href && pathname === "/profile") || (href !== "/admin" && href.startsWith("/admin/") && pathname.startsWith(`${href}/`))}
                leading={<ListIcon icon={icon} />}
                title={label}
                className="min-h-11"
              />
            ))}
          </nav>
        </>
      )}
    </aside>
  );
}
