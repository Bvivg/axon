"use client";

import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";

import { BrandMark } from "@/components/nav/brand";
import { ThemeToggle } from "@/components/nav/theme-toggle";
import { nextParam } from "@/lib/auth/guards";
import { cn } from "@/lib/utils";

const links = [
  { href: "/login", label: "Sign in" },
  { href: "/register", label: "Create account" },
];

export function AuthHeader() {
  const pathname = usePathname();
  const next = useSearchParams().get(nextParam);

  return (
    <header className="flex h-14 shrink-0 items-center gap-3 px-4">
      <span className="flex flex-1 items-center gap-2.5 text-sm font-bold tracking-[0.1em]"><BrandMark className="size-8 rounded-[10px]" /> AXON</span>
      <nav aria-label="Account access" className="flex gap-1 rounded-full bg-muted/70 p-1">
        {links.map(({ href, label }) => (
          <Link
            key={href}
            href={next ? { pathname: href, query: { [nextParam]: next } } : href}
            aria-current={pathname === href ? "page" : undefined}
            className={cn("rounded-full px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground", pathname === href && "bg-surface text-foreground shadow-raised")}
          >
            {label}
          </Link>
        ))}
      </nav>
      <ThemeToggle />
    </header>
  );
}
