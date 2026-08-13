"use client";

import { IdCard, MonitorSmartphone } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ComponentType, ReactNode } from "react";

import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { cn } from "@/lib/utils";

const items: { href: string; label: string; icon: ComponentType<{ className?: string }> }[] = [
  { href: "/profile/my", label: "Personal info", icon: IdCard },
  { href: "/profile/sessions", label: "Sessions", icon: MonitorSmartphone },
];

export default function ProfileLayout({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const isListRoute = pathname === "/profile";
  const { status } = useSession();

  useRequireSession();

  if (status !== "authenticated") {
    return (
      <main className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </main>
    );
  }

  return (
    <div className="flex min-h-screen w-full">
      <aside
        className={cn(
          "w-full shrink-0 overflow-y-auto md:sticky md:top-0 md:block md:min-h-screen md:w-72 md:self-start md:border-r md:border-border",
          isListRoute ? "block" : "hidden",
        )}
      >
        <ul className="divide-y divide-border">
          {items.map((item) => {
            const Icon = item.icon;
            const active = pathname === item.href;
            return (
              <li key={item.href}>
                <Link
                  href={item.href}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "flex items-center gap-3 px-6 py-4 text-sm font-medium transition-colors",
                    active
                      ? "bg-primary/5 text-primary"
                      : "text-foreground hover:bg-muted",
                  )}
                >
                  <Icon className="size-5 shrink-0" />
                  {item.label}
                </Link>
              </li>
            );
          })}
        </ul>
      </aside>

      <div
        className={cn("min-w-0 flex-1", isListRoute ? "hidden md:block" : "block")}
      >
        {children}
      </div>
    </div>
  );
}
