import { IdCard, MonitorSmartphone, Palette, UserRound, type LucideIcon } from "lucide-react";

export interface ProfilePage {
  href: string;
  label: string;
  icon: LucideIcon;
}

export const profileOverview: ProfilePage = { href: "/profile", label: "Overview", icon: UserRound };

export const profilePages: ProfilePage[] = [
  { href: "/profile/my", label: "Personal info", icon: IdCard },
  { href: "/profile/sessions", label: "Sessions", icon: MonitorSmartphone },
  { href: "/profile/appearance", label: "Appearance", icon: Palette },
];
