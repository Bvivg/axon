"use client";

import { LogOut, Pencil } from "lucide-react";
import Link from "next/link";

import { WorkspacePage } from "@/components/nav/workspace-page";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { PresenceDot } from "@/components/ui/status";
import { useSession } from "@/lib/auth/session";
import { calendarDate, fromTimestamp, initials, personName } from "@/lib/format";
import { ConnectionStatus, useConnectionStatus } from "@/lib/ws/connection";

export default function ProfileOverviewPage() {
  const { user, signOut } = useSession();
  const connection = useConnectionStatus();

  if (!user) {
    return null;
  }

  const name = personName(user);
  const online = connection === ConnectionStatus.Connected;
  const handle = user.username ? `@${user.username}` : user.email;
  const since = fromTimestamp(user.createdAt);

  return (
    <WorkspacePage
      title="My profile"
      back="/profile"
      backLabel="Back to profile"
      width="narrow"
      actions={<Link href="/profile/my" aria-label="Edit profile" className={buttonVariants({ variant: "ghost", size: "icon" })}><Pencil /></Link>}
    >
      <section className="flex items-center gap-4 rounded-[14px] border border-line bg-surface p-4">
        <Avatar
          self
          src={user.avatarUrls?.medium || user.avatarUrl}
          alt=""
          fallback={initials(name)}
          sizes="96px"
          className="size-16 rounded-[20px] text-xl"
        >
          <PresenceDot online={online} className="right-0 bottom-0 size-3.5" />
        </Avatar>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-lg font-semibold">{name}</h2>
          <p className={online ? "truncate text-xs text-signal" : "truncate text-xs text-muted-foreground"}>
            {online ? `${handle} · online` : handle}
          </p>
        </div>
      </section>

      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-3 rounded-[14px] border border-line bg-surface p-4 text-sm">
        <dt className="text-muted-foreground">Email</dt>
        <dd className="flex min-w-0 items-center gap-2">
          <span className="truncate">{user.email}</span>
          {user.emailVerified ? <Badge variant="success">Verified</Badge> : <Badge variant="warning">Unverified</Badge>}
        </dd>
        {since ? (
          <>
            <dt className="text-muted-foreground">Member since</dt>
            <dd>{calendarDate(since)}</dd>
          </>
        ) : null}
        {user.roles.length > 0 ? (
          <>
            <dt className="text-muted-foreground">Roles</dt>
            <dd className="flex flex-wrap gap-1.5">
              {user.roles.map((role) => <Badge key={role} variant="mono">{role}</Badge>)}
            </dd>
          </>
        ) : null}
      </dl>

      <Button variant="ghost" className="min-h-11 w-full justify-start gap-3 text-destructive hover:text-destructive" onClick={() => void signOut()}>
        <LogOut size={18} />Sign out
      </Button>
    </WorkspacePage>
  );
}
