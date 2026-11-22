"use client";

import { ArrowUpRight, LogOut, Menu } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState, type ReactNode } from "react";

import { BrandMark } from "@/components/shell/brand";
import { ThemeToggle } from "@/components/shell/theme-toggle";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { useAdminAccess } from "@/lib/admin/access";
import { leaveToSignIn } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { appHomeHref } from "@/lib/base-path";
import { initials } from "@/lib/format";
import { NavGroup, isActive, navGroupLabels, navItems } from "@/lib/navigation";

function ConsoleNav({ onNavigate }: { onNavigate?: () => void }) {
  const access = useAdminAccess();
  const pathname = usePathname();
  const visible = navItems.filter((item) => (!item.permission || access.can(item.permission)) && (!item.adminOnly || access.admin));
  return (
    <nav aria-label="Administration" className="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto">
      {Object.values(NavGroup).map((group) => {
        const items = visible.filter((item) => item.group === group);
        if (items.length === 0) return null;
        return (
          <div key={group} className="console-nav-group" role="group" aria-labelledby={`nav-${group}`}>
            {group === NavGroup.Console ? null : <span id={`nav-${group}`} className="console-nav-heading">{navGroupLabels[group]}</span>}
            {items.map(({ href, label, icon: Icon }) => (
              <Link key={href} href={href} onClick={onNavigate} className="console-nav-link" aria-current={isActive(href, pathname) ? "page" : undefined}>
                <Icon aria-hidden />
                {label}
              </Link>
            ))}
          </div>
        );
      })}
    </nav>
  );
}

function Brand() {
  return (
    <div className="flex items-center gap-2 px-2">
      <BrandMark className="size-7" />
      <span className="text-[15px] font-extrabold tracking-[1.5px]">AXON</span>
      <span className="rounded-md bg-accent px-1.5 py-0.5 font-mono text-2xs uppercase tracking-eyebrow text-signal">Admin</span>
    </div>
  );
}

function Account() {
  const { user, signOut } = useSession();
  const name = user?.displayName || user?.email || "";
  return (
    <div className="flex flex-col gap-1 border-t border-line pt-2">
      <a href={appHomeHref} className="console-nav-link">
        <ArrowUpRight aria-hidden />
        Open Axon
      </a>
      <div className="flex items-center gap-2 px-2 py-1">
        <Avatar src={user?.avatarUrls?.small || user?.avatarUrl} alt="" fallback={initials(name)} sizes="28px" className="size-7 rounded-[8px] text-[10px]" />
        <span className="min-w-0 flex-1 truncate text-[13px]">{name}</span>
        <ThemeToggle />
        <Button variant="ghost" size="icon" aria-label="Sign out" onClick={() => void signOut().then(leaveToSignIn)}>
          <LogOut />
        </Button>
      </div>
    </div>
  );
}

export function ConsoleShell({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="console-shell">
      <aside className="console-sidebar">
        <Brand />
        <ConsoleNav />
        <Account />
      </aside>
      <div className="console-topbar">
        <Button variant="ghost" size="icon" aria-label="Open navigation" aria-expanded={open} onClick={() => setOpen(true)}>
          <Menu />
        </Button>
        <Brand />
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex h-[min(640px,90dvh)] flex-col gap-3 p-3">
          <DialogTitle className="sr-only">Navigation</DialogTitle>
          <ConsoleNav onNavigate={() => setOpen(false)} />
          <Account />
        </DialogContent>
      </Dialog>
      <div id="console-content" className="console-main">{children}</div>
    </div>
  );
}
