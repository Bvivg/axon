import { Gamepad2, MessageCircle, Phone, User, type LucideIcon } from "lucide-react";

/**
 * The four destinations behind an account, and the search action that sits
 * apart from them.
 *
 * A section is also what decides what the search button does — see
 * search-panel.tsx. Deriving both the active nav state and the search
 * behaviour from the same match keeps a room page (`/chat/<id>`) reading as
 * part of "Chats" in both places at once, rather than two lists that can
 * drift.
 */
export type Section = "games" | "calls" | "chats" | "profile";

export interface NavItem {
  section: Section;
  href: string;
  label: string;
  icon: LucideIcon;
  /** Whether this item is the current destination, given the pathname. */
  isActive: (pathname: string) => boolean;
}

export const navItems: NavItem[] = [
  {
    section: "games",
    href: "/",
    label: "Games",
    icon: Gamepad2,
    // Exact only: every other route also starts with "/", so a prefix match
    // would light this up everywhere.
    isActive: (pathname) => pathname === "/",
  },
  {
    section: "calls",
    href: "/calls",
    label: "Calls",
    icon: Phone,
    isActive: (pathname) => pathname === "/calls" || pathname.startsWith("/calls/"),
  },
  {
    section: "chats",
    href: "/chat",
    label: "Chats",
    icon: MessageCircle,
    isActive: (pathname) => pathname === "/chat" || pathname.startsWith("/chat/"),
  },
  {
    section: "profile",
    href: "/profile",
    label: "Profile",
    icon: User,
    isActive: (pathname) => pathname === "/profile" || pathname.startsWith("/profile/"),
  },
];

/** sectionFor reads which section a pathname belongs to, if any of them. */
export function sectionFor(pathname: string): Section | null {
  return navItems.find((item) => item.isActive(pathname))?.section ?? null;
}
