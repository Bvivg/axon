"use client";

import { Search } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState, type ReactNode } from "react";

import { navItems, sectionFor, type NavItem } from "@/components/nav/items";
import { SearchPanel } from "@/components/nav/search-panel";
import { cn } from "@/lib/utils";

export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const section = sectionFor(pathname);
  const [searchOpen, setSearchOpen] = useState(false);

  return (
    <div className="flex w-full bg-background">
      <nav
        aria-label="Primary"
        className="hidden md:sticky md:top-0 md:flex md:min-h-screen md:w-20 md:shrink-0 md:flex-col md:items-center md:gap-1 md:self-start md:border-r md:border-border md:py-6"
      >
        <Link
          href="/"
          aria-label="Axon"
          className="mb-6 flex size-10 items-center justify-center rounded-full bg-primary text-sm font-semibold text-primary-foreground"
        >
          Ax
        </Link>

        {navItems.map((item) => (
          <SidebarTab key={item.href} item={item} active={item.isActive(pathname)} />
        ))}

        <div className="mt-auto pt-4">
          <SearchTrigger variant="sidebar" onOpen={() => setSearchOpen(true)} />
        </div>
      </nav>

      <div className="flex min-h-screen flex-1 flex-col pb-28 md:pb-0">{children}</div>

      <div className="pointer-events-none fixed inset-x-0 bottom-0 z-40 flex items-center justify-center gap-3 px-4 pb-[max(1rem,env(safe-area-inset-bottom))] md:hidden">
        <nav
          aria-label="Primary"
          className="pointer-events-auto flex flex-1 items-center justify-between rounded-full border border-border bg-card/95 px-1 py-1.5 shadow-lg backdrop-blur"
        >
          {navItems.map((item) => (
            <MobileTab key={item.href} item={item} active={item.isActive(pathname)} />
          ))}
        </nav>

        <div className="pointer-events-auto">
          <SearchTrigger variant="floating" onOpen={() => setSearchOpen(true)} />
        </div>
      </div>

      {searchOpen && section ? (
        <SearchPanel section={section} onClose={() => setSearchOpen(false)} />
      ) : null}
    </div>
  );
}

function SidebarTab({ item, active }: { item: NavItem; active: boolean }) {
  const Icon = item.icon;
  return (
    <Link
      href={item.href}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex w-16 flex-col items-center gap-1 rounded-lg py-2 text-[11px] transition-colors",
        active
          ? "bg-primary/10 text-primary"
          : "text-muted-foreground hover:bg-muted hover:text-foreground",
      )}
    >
      <Icon className="size-5" />
      {item.label}
    </Link>
  );
}

function MobileTab({ item, active }: { item: NavItem; active: boolean }) {
  const Icon = item.icon;
  return (
    <Link
      href={item.href}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex flex-1 flex-col items-center gap-0.5 rounded-full py-1.5 text-[10px] transition-colors",
        active ? "text-primary" : "text-muted-foreground",
      )}
    >
      <Icon className="size-5" />
      {item.label}
    </Link>
  );
}

function SearchTrigger({
  variant,
  onOpen,
}: {
  variant: "sidebar" | "floating";
  onOpen: () => void;
}) {
  if (variant === "sidebar") {
    return (
      <button
        onClick={onOpen}
        aria-label="Search"
        className="flex size-11 items-center justify-center rounded-full border border-border text-muted-foreground transition-colors hover:border-primary hover:text-primary"
      >
        <Search className="size-5" />
      </button>
    );
  }

  return (
    <button
      onClick={onOpen}
      aria-label="Search"
      className="flex size-14 shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground shadow-lg transition-transform active:scale-95"
    >
      <Search className="size-5" />
    </button>
  );
}
