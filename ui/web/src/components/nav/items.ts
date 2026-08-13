import { Gamepad2, MessageCircle, Phone, User, type LucideIcon } from "lucide-react";

export type Section = "games" | "calls" | "chats" | "profile";

export interface NavItem {
  section: Section;
  href: string;
  label: string;
  icon: LucideIcon;

  isActive: (pathname: string) => boolean;
}

export const navItems: NavItem[] = [
  {
    section: "games",
    href: "/",
    label: "Games",
    icon: Gamepad2,

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

export function sectionFor(pathname: string): Section | null {
  return navItems.find((item) => item.isActive(pathname))?.section ?? null;
}
