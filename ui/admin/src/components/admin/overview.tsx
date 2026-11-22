"use client";

import { ArrowRight, type LucideIcon } from "lucide-react";
import Link from "next/link";

import { AuditList } from "@/components/admin/audit-table";
import { navItems as adminNavItems } from "@/lib/navigation";
import { Pane as WorkspacePage } from "@/components/ui/pane";
import { buttonVariants } from "@/components/ui/button";
import { PaneSection } from "@/components/ui/pane";
import { Skeleton } from "@/components/ui/skeleton";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { featureOff, useAdminCount, type AdminCounts } from "@/lib/query/admin";

function Stat({ label, metric, permission, href, icon: Icon }: { label: string; metric: keyof AdminCounts; permission: Permission; href: string; icon: LucideIcon }) {
  const access = useAdminAccess();
  const allowed = access.can(permission);
  const value = useAdminCount(metric, allowed);
  if (!allowed) return null;
  return (
    <Link href={href} className="group flex flex-col gap-2 rounded-xl border border-line bg-surface p-4 transition-colors hover:bg-accent/50">
      <span className="flex items-center justify-between text-sm text-muted-foreground"><span className="flex items-center gap-2"><Icon aria-hidden className="size-4" />{label}</span><ArrowRight aria-hidden className="size-4 opacity-0 transition-opacity group-hover:opacity-100" /></span>
      {value.isPending ? <Skeleton className="h-8 w-20" /> : featureOff(value.error) ? <span className="text-sm text-muted-foreground">Off</span> : value.isError ? <span className="text-sm text-destructive">Unavailable</span> : <span className="text-2xl font-semibold tabular-nums">{Number(value.data).toLocaleString()}</span>}
    </Link>
  );
}

const icon = (href: string) => adminNavItems.find((item) => item.href === href)!.icon;

export function AdminOverview() {
  const access = useAdminAccess();
  const sections = adminNavItems.filter((item) => item.href !== "/" && (!item.permission || access.can(item.permission)) && (!item.adminOnly || access.admin));
  return (
    <WorkspacePage title="Administration" width="wide">
      <section aria-label="Key numbers" className="grid grid-cols-[repeat(auto-fill,minmax(11rem,1fr))] gap-3">
        <Stat label="Accounts" metric="users" permission={Permission.UsersRead} href="/users" icon={icon("/users")} />
        <Stat label="Suspended" metric="suspended" permission={Permission.UsersRead} href="/users?f.status=suspended" icon={icon("/users")} />
        <Stat label="Live games" metric="liveGames" permission={Permission.GamesRead} href="/games?f.status=live" icon={icon("/games")} />
        <Stat label="Calls in progress" metric="activeCalls" permission={Permission.ChatRead} href="/calls?f.active=true" icon={icon("/calls")} />
        <Stat label="Chats" metric="rooms" permission={Permission.ChatRead} href="/chats" icon={icon("/chats")} />
      </section>
      {access.can(Permission.AuditRead) ? (
        <PaneSection id="recent" title="Recent admin activity" aside={<Link href="/audit" className={buttonVariants({ variant: "link", size: "sm" })}>Audit log</Link>}>
          <AuditList filters={{}} showTarget />
        </PaneSection>
      ) : null}
      <nav aria-label="Administration sections" className="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-2 min-[721px]:hidden">
        {sections.map(({ href, label, icon: Icon }) => (
          <Link key={href} href={href} className={buttonVariants({ variant: "outline", className: "h-11 justify-start" })}><Icon aria-hidden /> {label}</Link>
        ))}
      </nav>
    </WorkspacePage>
  );
}
