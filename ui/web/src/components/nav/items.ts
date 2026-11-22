import { Gamepad2, Images, LayoutDashboard, MessageSquare, Phone, User, Users, type LucideIcon } from "lucide-react";

export enum Section {
  Lobby = "lobby",
  Chats = "chats",
  Calls = "calls",
  Contacts = "contacts",
  Media = "media",
  Games = "games",
  Profile = "profile",
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

export const adminConsoleHref = "/admin";

export const adminConsoleItem = { href: adminConsoleHref, label: "Administration", icon: LayoutDashboard };

const sectionPrefixes: [Section, string][] = [
  [Section.Games, "/games"],
  [Section.Chats, "/chat"],
  [Section.Calls, "/calls"],
  [Section.Contacts, "/contacts"],
  [Section.Media, "/media"],
  [Section.Profile, "/profile"],
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
};
