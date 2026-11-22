"use client";

import { Code } from "@connectrpc/connect";
import { Ban, KeyRound, Link2Off, LogOut, Pencil, RotateCcw, UserX } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { AuditList } from "@/components/admin/audit-table";
import { Mono, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { ProfileDialog, RolesDialog } from "@/components/admin/users/user-dialogs";
import { StatusBadge } from "@/components/admin/users-table";
import { Pane as WorkspacePage } from "@/components/ui/pane";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { PaneSection } from "@/components/ui/pane";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { UserStatus, type LinkedAccount } from "@/gen/axon/auth/v1/admin_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { useSession } from "@/lib/auth/session";
import { isCode } from "@/lib/errors";
import { initials, personName } from "@/lib/format";
import { useAdminUser, useRestoreUser, useRevokeUserSessions, useSuspendUser, useUnlinkAccount } from "@/lib/query/admin";
import { useAdminRoles } from "@/lib/query/roles";

enum Pending {
  None = "",
  Suspend = "suspend",
  Restore = "restore",
  Sessions = "sessions",
  Roles = "roles",
  Profile = "profile",
  Unlink = "unlink",
}

function Fact({ term, children }: { term: string; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{term}</dt>
      <dd className="min-w-0 text-sm break-words">{children}</dd>
    </div>
  );
}

export function UserDetail({ id }: { id: string }) {
  const access = useAdminAccess();
  const { user: me } = useSession();
  const detail = useAdminUser(id);
  const roles = useAdminRoles(access.can(Permission.RolesRead));
  const suspend = useSuspendUser();
  const restore = useRestoreUser();
  const revokeSessions = useRevokeUserSessions();
  const unlink = useUnlinkAccount();
  const [pending, setPending] = useState<Pending>(Pending.None);
  const [account, setAccount] = useState<LinkedAccount>();

  if (detail.isPending) {
    return (
      <WorkspacePage title="Account" back="/users" backLabel="Back to users" width="wide">
        <div role="status" aria-label="Loading account" className="space-y-3"><Skeleton className="h-28 w-full" /><Skeleton className="h-40 w-full" /></div>
      </WorkspacePage>
    );
  }

  if (detail.isError || !detail.data.user?.user) {
    return (
      <WorkspacePage title="Account" back="/users" backLabel="Back to users" width="wide">
        {isCode(detail.error, Code.NotFound) ? <EmptyState icon={UserX} title="This account does not exist" /> : <ErrorCard title="Could not load the account" error={detail.error} onRetry={() => void detail.refetch()} />}
      </WorkspacePage>
    );
  }

  const admin = detail.data.user;
  const user = admin.user!;
  const name = personName(user);
  const self = me?.id === user.id;
  const suspended = admin.status === UserStatus.SUSPENDED;
  const mutate = access.can(Permission.UsersMutate) && !self;
  const sessions = detail.data.sessions;
  const linked = detail.data.linkedAccounts;
  const editRoles = access.can(Permission.RolesMutate) && access.can(Permission.UsersRead);

  return (
    <WorkspacePage
      title={name}
      back="/users"
      backLabel="Back to users"
      width="wide"
      actions={mutate ? (
        <>
          {suspended ? (
            <Button variant="outline" size="sm" onClick={() => setPending(Pending.Restore)}><RotateCcw aria-hidden /> Restore</Button>
          ) : (
            <Button variant="destructive-outline" size="sm" onClick={() => setPending(Pending.Suspend)}><Ban aria-hidden /> Suspend</Button>
          )}
          <Button variant="outline" size="sm" disabled={sessions.length === 0} onClick={() => setPending(Pending.Sessions)}><LogOut aria-hidden /> Sign out everywhere</Button>
        </>
      ) : undefined}
    >
      <PaneSection id="account" title="Account" aside={<StatusBadge user={admin} />}>
        <div className="flex items-start gap-4">
          <Avatar src={user.avatarUrls?.medium || user.avatarUrl} alt="" fallback={initials(name)} sizes="56px" className="size-14 rounded-[16px]" />
          <dl className="grid min-w-0 flex-1 grid-cols-[repeat(auto-fill,minmax(12rem,1fr))] gap-x-6 gap-y-3">
            <Fact term="Email">{user.email} {user.emailVerified ? <Badge variant="success">verified</Badge> : <Badge>unverified</Badge>}</Fact>
            <Fact term="Username">{user.username ? `@${user.username}` : "—"}</Fact>
            <Fact term="Display name">
              <span className="inline-flex items-center gap-1.5">
                {user.displayName || "—"}
                {access.can(Permission.UsersMutate) ? (
                  <Button variant="ghost" size="icon" className="size-6" aria-label="Edit display name" onClick={() => setPending(Pending.Profile)}><Pencil aria-hidden className="size-3.5" /></Button>
                ) : null}
              </span>
            </Fact>
            <Fact term="Joined"><When at={user.createdAt} /></Fact>
            <Fact term="Last seen"><When at={admin.lastSeenAt} /></Fact>
            <Fact term="User ID"><Mono>{user.id}</Mono></Fact>
            {suspended ? <Fact term="Suspended"><When at={admin.suspendedAt} /></Fact> : null}
            {suspended ? <Fact term="Suspension reason">{admin.suspendedReason}</Fact> : null}
          </dl>
        </div>
      </PaneSection>

      <PaneSection
        id="roles"
        title="Roles"
        aside={editRoles ? <Button variant="outline" size="sm" onClick={() => setPending(Pending.Roles)}><KeyRound aria-hidden /> Edit roles</Button> : undefined}
      >
        {user.roles.length === 0 ? <p className="text-sm text-muted-foreground">No roles</p> : (
          <ul aria-label="Granted roles" className="flex flex-wrap gap-1.5">
            {user.roles.map((name) => {
              const role = roles.data?.find((candidate) => candidate.name === name);
              return <li key={name}><Badge variant={name === "admin" ? "signal" : "outline"}>{role?.title || name}</Badge></li>;
            })}
          </ul>
        )}
      </PaneSection>

      <PaneSection id="sign-in" title="Sign-in methods">
        <ul className="divide-y divide-line">
          <li className="flex items-center justify-between gap-3 py-2.5 text-sm">
            <span>Password</span>
            {detail.data.hasPassword ? <Badge variant="success">set</Badge> : <Badge>not set</Badge>}
          </li>
          {linked.map((item) => (
            <li key={item.provider} className="flex items-center justify-between gap-3 py-2.5 text-sm">
              <span className="flex min-w-0 flex-col">
                <span className="capitalize">{item.provider}</span>
                <span className="truncate text-xs text-muted-foreground">{item.email || "no email"} · linked <When at={item.linkedAt} /></span>
              </span>
              {access.can(Permission.UsersMutate) && !self ? (
                <Button variant="destructive-outline" size="sm" onClick={() => {
                  setAccount(item);
                  setPending(Pending.Unlink);
                }}><Link2Off aria-hidden /> Unlink</Button>
              ) : null}
            </li>
          ))}
        </ul>
      </PaneSection>

      <PaneSection id="sessions" title={`Active sessions · ${sessions.length}`}>
        {sessions.length === 0 ? <p className="text-sm text-muted-foreground">No active sessions</p> : (
          <ScrollArea orientation="horizontal">
            <table aria-label="Active sessions" className="w-full min-w-[36rem] text-left text-sm">
              <thead className="text-xs text-muted-foreground"><tr><th scope="col" className="py-2 pr-3 font-medium">Device</th><th scope="col" className="py-2 pr-3 font-medium">IP</th><th scope="col" className="py-2 pr-3 font-medium">Started</th><th scope="col" className="py-2 font-medium">Last used</th></tr></thead>
              <tbody>
                {sessions.map((session) => (
                  <tr key={session.id} className="border-t border-line">
                    <td className="max-w-[22rem] truncate py-2 pr-3" title={session.userAgent}>{session.userAgent || "Unknown device"}{session.online ? <Badge variant="success" className="ml-2">online</Badge> : null}</td>
                    <td className="py-2 pr-3"><Mono>{session.ip || "—"}</Mono></td>
                    <td className="py-2 pr-3"><When at={session.startedAt} /></td>
                    <td className="py-2"><When at={session.lastUsedAt} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </ScrollArea>
        )}
      </PaneSection>

      {access.can(Permission.AuditRead) ? (
        <>
          <PaneSection id="history" title="Actions on this account" aside={<Link className={buttonVariants({ variant: "link", size: "sm" })} href={`/audit?targetType=user&target=${user.id}`}>Open in audit log</Link>}>
            <AuditList filters={{ targetType: "user", target: user.id }} />
          </PaneSection>
          <PaneSection id="performed" title="Actions by this account" aside={<Link className={buttonVariants({ variant: "link", size: "sm" })} href={`/audit?actor=${user.id}`}>Open in audit log</Link>}>
            <AuditList filters={{ actor: user.id }} />
          </PaneSection>
        </>
      ) : null}

      <ConfirmAction
        open={pending === Pending.Suspend}
        onOpenChange={(open) => setPending(open ? Pending.Suspend : Pending.None)}
        title={`Suspend ${name}?`}
        consequence="They are signed out on every device and cannot sign in until restored."
        confirmLabel="Suspend account"
        onConfirm={(reason) => suspend.mutateAsync({ userId: user.id, reason })}
      />
      <ConfirmAction
        open={pending === Pending.Restore}
        onOpenChange={(open) => setPending(open ? Pending.Restore : Pending.None)}
        title={`Restore ${name}?`}
        consequence="They can sign in again."
        confirmLabel="Restore account"
        destructive={false}
        onConfirm={(reason) => restore.mutateAsync({ userId: user.id, reason })}
      />
      <ConfirmAction
        open={pending === Pending.Sessions}
        onOpenChange={(open) => setPending(open ? Pending.Sessions : Pending.None)}
        title={`Sign ${name} out everywhere?`}
        consequence={`${sessions.length} active session${sessions.length === 1 ? "" : "s"} end; open chats disconnect.`}
        confirmLabel="Sign out everywhere"
        onConfirm={(reason) => revokeSessions.mutateAsync({ userId: user.id, reason })}
      />
      <ConfirmAction
        open={pending === Pending.Unlink && !!account}
        onOpenChange={(open) => setPending(open ? Pending.Unlink : Pending.None)}
        title={`Unlink ${account?.provider ?? ""} from ${name}?`}
        consequence="They can no longer sign in with this provider until they link it again."
        confirmLabel="Unlink account"
        onConfirm={async (reason) => {
          if (!account) return;
          await unlink.mutateAsync({ userId: user.id, provider: account.provider, reason });
        }}
      />
      <ProfileDialog open={pending === Pending.Profile} onOpenChange={(open) => setPending(open ? Pending.Profile : Pending.None)} user={admin} onReload={async () => (await detail.refetch()).data?.user} />
      <RolesDialog open={pending === Pending.Roles} onOpenChange={(open) => setPending(open ? Pending.Roles : Pending.None)} user={admin} roles={roles.data} lockAdmin={self} />
    </WorkspacePage>
  );
}
