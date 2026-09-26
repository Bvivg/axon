"use client";

import { ChevronRight, LogOut } from "lucide-react";
import Link from "next/link";

import { profilePages } from "@/components/profile/profile-nav";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { PresenceDot } from "@/components/ui/status";
import { useSession } from "@/lib/auth/session";
import { fromTimestamp, initials, personName } from "@/lib/format";
import { ConnectionStatus, useConnectionStatus } from "@/lib/ws/connection";

export default function ProfileOverviewPage() {
  const { user, signOut } = useSession();
  const connection = useConnectionStatus();

  if (!user) {
    return null;
  }

  const name = personName(user);
  const online = connection === ConnectionStatus.Connected;
  const handle = user.nickname ? `@${user.nickname}` : user.email;
  const since = fromTimestamp(user.createdAt);

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-8 px-4 py-8 md:px-8 md:py-10">
      <section className="flex flex-col items-center gap-3 text-center md:flex-row md:gap-4 md:text-left">
        <Avatar
          self
          src={user.avatarUrls?.medium || user.avatarUrl}
          alt=""
          fallback={initials(name)}
          sizes="96px"
          className="size-24 text-2xl md:size-16 md:text-lg"
        >
          <PresenceDot online={online} className="right-1 bottom-1 size-3.5 md:right-0 md:bottom-0 md:size-3" />
        </Avatar>
        <div className="flex flex-col gap-0.5">
          <h1 className="text-xl font-semibold tracking-snug">{name}</h1>
          <span className="font-mono text-sm text-muted-foreground">
            {online ? `${handle} · online` : handle}
          </span>
        </div>
      </section>

      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-3 rounded-lg border border-border p-4 text-sm">
        <dt className="text-muted-foreground">Email</dt>
        <dd className="flex min-w-0 items-center gap-2">
          <span className="truncate">{user.email}</span>
          {user.emailVerified ? (
            <Badge variant="success">Verified</Badge>
          ) : (
            <Badge variant="warning">Unverified</Badge>
          )}
        </dd>
        {since ? (
          <>
            <dt className="text-muted-foreground">Member since</dt>
            <dd className="font-mono text-xs leading-5">
              {since.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })}
            </dd>
          </>
        ) : null}
        {user.roles.length > 0 ? (
          <>
            <dt className="text-muted-foreground">Roles</dt>
            <dd className="flex flex-wrap gap-1.5">
              {user.roles.map((role) => (
                <Badge key={role} variant="mono">
                  {role}
                </Badge>
              ))}
            </dd>
          </>
        ) : null}
      </dl>

      <ul className="flex flex-col overflow-hidden rounded-lg border border-border md:hidden">
        {profilePages.map((page) => {
          const Icon = page.icon;
          return (
            <li key={page.href} className="border-b border-border last:border-b-0">
              <Link href={page.href} className="flex min-h-14 items-center gap-3 px-4 text-md hover:bg-accent">
                <Icon aria-hidden className="size-5 text-muted-foreground" strokeWidth={1.5} />
                <span className="flex-1">{page.label}</span>
                <ChevronRight aria-hidden className="size-4 text-muted-foreground" strokeWidth={1.5} />
              </Link>
            </li>
          );
        })}
      </ul>

      <Button variant="destructive-outline" size="lg" className="w-full md:hidden" onClick={() => void signOut()}>
        <LogOut strokeWidth={1.5} />
        Sign out
      </Button>
    </div>
  );
}
