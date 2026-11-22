"use client";

import { ShieldAlert } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

import { Avatar } from "@/components/ui/avatar";
import { EmptyState } from "@/components/ui/empty-state";
import { PaneHeader } from "@/components/ui/pane";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { AccessStatus, Permission, useAdminAccess } from "@/lib/admin/access";
import { calendarDate, clockTime, fromTimestamp, initials, profileName, shortID } from "@/lib/format";
import { games, gameKeys } from "@/lib/games/catalog";
import { cn } from "@/lib/utils";

export function AdminListPage({ title, subtitle, actions, toolbar, children }: { title: string; subtitle?: ReactNode; actions?: ReactNode; toolbar?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <PaneHeader title={title} subtitle={subtitle} actions={actions} back="/" backLabel="Back to administration" />
      {toolbar}
      <div className="flex min-h-0 flex-1 flex-col">{children}</div>
    </div>
  );
}

export function RequirePermission({ permission, children }: { permission: Permission; children: ReactNode }) {
  const access = useAdminAccess();
  if (access.status !== AccessStatus.Ready) return null;
  if (!access.can(permission)) {
    return (
      <main className="flex min-h-0 flex-1 items-center justify-center">
        <EmptyState icon={ShieldAlert} title="You do not have access to this page" headingLevel="h1" />
      </main>
    );
  }
  return children;
}

export function RequireAdmin({ children }: { children: ReactNode }) {
  const access = useAdminAccess();
  if (access.status !== AccessStatus.Ready) return null;
  if (!access.admin) {
    return (
      <main className="flex min-h-0 flex-1 items-center justify-center">
        <EmptyState icon={ShieldAlert} title="Only administrators can open this page" headingLevel="h1" />
      </main>
    );
  }
  return children;
}

export function When({ at, className }: { at?: Timestamp; className?: string }) {
  const date = fromTimestamp(at);
  if (!date) return <span className="text-muted-foreground">—</span>;
  return (
    <time dateTime={date.toISOString()} title={date.toLocaleString()} className={cn("whitespace-nowrap tabular-nums", className)}>
      {calendarDate(date)} {clockTime(date)}
    </time>
  );
}

export function userHref(id: string): string {
  return `/users/${id}`;
}

export function Person({ id, profiles, linked = true }: { id?: string; profiles?: Map<string, PublicProfile>; linked?: boolean }) {
  const access = useAdminAccess();
  if (!id) return <span className="text-muted-foreground">—</span>;
  const profile = profiles?.get(id);
  const name = profileName(profile) || shortID(id);
  const body = (
    <>
      <Avatar src={profile?.avatarUrls?.small || profile?.avatarUrl} alt="" fallback={initials(name)} sizes="24px" className="size-6 rounded-[8px] text-[10px]" />
      <span className="min-w-0 truncate">{name}</span>
    </>
  );
  if (linked && access.can(Permission.UsersRead)) {
    return <Link href={userHref(id)} className="inline-flex max-w-[16rem] items-center gap-2 hover:underline">{body}</Link>;
  }
  return <span className="inline-flex max-w-[16rem] items-center gap-2">{body}</span>;
}

export function Mono({ children, className }: { children: ReactNode; className?: string }) {
  return <span className={cn("font-mono text-xs text-muted-foreground", className)}>{children}</span>;
}

export const gameOptions = [{ value: "", label: "All games" }, ...gameKeys.map((key) => ({ value: key, label: games[key].title }))];

export function gameTitle(key: string): string {
  return gameKeys.find((candidate) => candidate === key) ? games[key as keyof typeof games].title : key;
}

export function count(total: bigint | undefined, noun: string): string | undefined {
  if (total === undefined) return undefined;
  const value = Number(total);
  return `${value.toLocaleString()} ${noun}${value === 1 ? "" : "s"}`;
}
