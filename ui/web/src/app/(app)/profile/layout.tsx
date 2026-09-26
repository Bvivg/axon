"use client";

import { LogOut } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

import { profileOverview, profilePages } from "@/components/profile/profile-nav";
import { useSession } from "@/lib/auth/session";
import { cn } from "@/lib/utils";

const rowClass =
  "flex h-8 items-center gap-2.5 rounded-md px-2 text-sm transition-colors duration-[120ms] ease-signal hover:bg-accent";

export default function ProfileLayout({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const { signOut } = useSession();

  return (
    <div className="flex min-h-0 flex-1">
      <nav
        aria-label="Profile"
        className="hidden w-56 shrink-0 flex-col gap-px border-r border-border px-2 py-3 md:flex"
      >
        <h2 className="eyebrow px-2 pb-1.5">Account</h2>
        {[profileOverview, ...profilePages].map((page) => {
          const Icon = page.icon;
          const active = pathname === page.href;
          return (
            <Link
              key={page.href}
              href={page.href}
              aria-current={active ? "page" : undefined}
              className={cn(rowClass, active ? "bg-accent font-medium" : "text-muted-foreground")}
            >
              <Icon aria-hidden className="size-4 shrink-0" strokeWidth={1.5} />
              {page.label}
            </Link>
          );
        })}
        <span className="flex-1" />
        <button
          type="button"
          onClick={() => void signOut()}
          className={cn(rowClass, "text-muted-foreground hover:text-destructive")}
        >
          <LogOut aria-hidden className="size-4 shrink-0" strokeWidth={1.5} />
          Sign out
        </button>
      </nav>

      <div className="min-w-0 flex-1 overflow-y-auto">{children}</div>
    </div>
  );
}
