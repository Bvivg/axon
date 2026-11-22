import { Cpu, Gamepad2, Hourglass, KeyRound, LayoutDashboard, MessagesSquare, Network, Paperclip, PhoneCall, Route, ScrollText, Trophy, TrendingUp, Tv, UserCog, type LucideIcon } from "lucide-react";

import { Permission } from "@/lib/admin/permissions";

export enum NavGroup {
  Console = "console",
  People = "people",
  Play = "play",
  Messaging = "messaging",
  System = "system",
}

export interface NavItem {
  group: NavGroup;
  href: string;
  label: string;
  icon: LucideIcon;
  permission?: Permission;
  adminOnly?: boolean;
}

export const navGroupLabels: Record<NavGroup, string> = {
  [NavGroup.Console]: "Console",
  [NavGroup.People]: "People",
  [NavGroup.Play]: "Play",
  [NavGroup.Messaging]: "Messaging",
  [NavGroup.System]: "System",
};

export const navItems: NavItem[] = [
  { group: NavGroup.Console, href: "/", label: "Overview", icon: LayoutDashboard },
  { group: NavGroup.People, href: "/users", label: "Users", icon: UserCog, permission: Permission.UsersRead },
  { group: NavGroup.People, href: "/roles", label: "Roles", icon: KeyRound, permission: Permission.RolesRead },
  { group: NavGroup.Play, href: "/games", label: "Games", icon: Gamepad2, permission: Permission.GamesRead },
  { group: NavGroup.Play, href: "/games/tv", label: "Axon TV", icon: Tv, permission: Permission.GamesRead },
  { group: NavGroup.Play, href: "/queue", label: "Match queue", icon: Hourglass, permission: Permission.GamesRead },
  { group: NavGroup.Play, href: "/tournaments", label: "Tournaments", icon: Trophy, permission: Permission.GamesRead },
  { group: NavGroup.Play, href: "/ratings", label: "Ratings", icon: TrendingUp, permission: Permission.GamesRead },
  { group: NavGroup.Play, href: "/analysis", label: "Analysis", icon: Cpu, permission: Permission.GamesRead },
  { group: NavGroup.Messaging, href: "/chats", label: "Chats", icon: MessagesSquare, permission: Permission.ChatRead },
  { group: NavGroup.Messaging, href: "/uploads", label: "Uploads", icon: Paperclip, permission: Permission.ChatRead },
  { group: NavGroup.Messaging, href: "/calls", label: "Calls", icon: PhoneCall, permission: Permission.ChatRead },
  { group: NavGroup.System, href: "/audit", label: "Audit log", icon: ScrollText, permission: Permission.AuditRead },
  { group: NavGroup.System, href: "/service-map", label: "Service map", icon: Network, adminOnly: true },
  { group: NavGroup.System, href: "/gateway", label: "Gateway", icon: Route, adminOnly: true },
];

const within = (href: string, pathname: string) => pathname === href || pathname.startsWith(`${href}/`);

export function isActive(href: string, pathname: string): boolean {
  if (href === "/") return pathname === "/";
  return within(href, pathname) && !navItems.some(item => item.href.startsWith(`${href}/`) && within(item.href, pathname));
}
