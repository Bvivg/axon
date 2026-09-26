import { LayoutGrid, MessageSquare, Phone, User, type LucideIcon } from "lucide-react";

export enum Section {
  Lobby = "lobby",
  Chats = "chats",
  Calls = "calls",
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
  { section: Section.Lobby, href: "/", label: "Lobby", icon: LayoutGrid, keys: ["G", "L"] },
  { section: Section.Chats, href: "/chat", label: "Chats", icon: MessageSquare, keys: ["G", "C"] },
  { section: Section.Calls, href: "/calls", label: "Calls", icon: Phone },
];

export const profileItem: NavItem = {
  section: Section.Profile,
  href: "/profile",
  label: "Profile",
  icon: User,
};

const sectionPrefixes: [Section, string][] = [
  [Section.Chats, "/chat"],
  [Section.Calls, "/calls"],
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
  [Section.Lobby]: "Lobby",
  [Section.Chats]: "Chats",
  [Section.Calls]: "Calls",
  [Section.Profile]: "Profile",
};
