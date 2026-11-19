import { Gamepad2, Images, MessageSquare, Network, Phone, Route, ShieldCheck, User, Users, type LucideIcon } from "lucide-react";

export enum Section {
  Lobby = "lobby",
  Chats = "chats",
  Calls = "calls",
  Contacts = "contacts",
  Media = "media",
  Games = "games",
  Profile = "profile",
  Admin = "admin",
  ServiceMap = "service-map",
  Gateway = "gateway",
}

export interface NavItem {
  section: Section;
  href: string;
  label: string;
  icon: LucideIcon;
  keys?: string[];
}

export const navItems: NavItem[] = [
  { section: Section.Games, href: "/games", label: "Games", icon: Gamepad2 },
  { section: Section.Calls, href: "/calls", label: "Calls", icon: Phone },
  { section: Section.Chats, href: "/chat", label: "Chats", icon: MessageSquare, keys: ["G", "C"] },
  { section: Section.Profile, href: "/profile", label: "Profile", icon: User },
];

export const chatNavItems: NavItem[] = [
  { section: Section.Contacts, href: "/contacts", label: "Contacts", icon: Users },
  { section: Section.Media, href: "/media", label: "Media", icon: Images },
];

export const adminNavItems: NavItem[] = [
  { section: Section.Admin, href: "/admin", label: "Admin", icon: ShieldCheck },
  { section: Section.ServiceMap, href: "/admin/service-map", label: "Service map", icon: Network },
  { section: Section.Gateway, href: "/admin/gateway", label: "Gateway", icon: Route },
];

const sectionPrefixes: [Section, string][] = [
  [Section.Games, "/games"],
  [Section.Chats, "/chat"],
  [Section.Calls, "/calls"],
  [Section.Contacts, "/contacts"],
  [Section.Media, "/media"],
  [Section.Profile, "/profile"],
  [Section.ServiceMap, "/admin/service-map"],
  [Section.Gateway, "/admin/gateway"],
  [Section.Admin, "/admin"],
];

export function sectionFor(pathname: string): Section | null {
  if (pathname === "/") {
    return Section.Lobby;
  }
  for (const [section, prefix] of sectionPrefixes) {
    if (pathname === prefix || pathname.startsWith(`${prefix}/`)) {
      return section;
    }
  }
  return null;
}

export const sectionLabels: Record<Section, string> = {
  [Section.Lobby]: "Overview",
  [Section.Chats]: "Chats",
  [Section.Calls]: "Calls",
  [Section.Contacts]: "Contacts",
  [Section.Media]: "Media",
  [Section.Games]: "Games",
  [Section.Profile]: "Profile",
  [Section.Admin]: "Admin",
  [Section.ServiceMap]: "Service map",
  [Section.Gateway]: "Gateway",
};
