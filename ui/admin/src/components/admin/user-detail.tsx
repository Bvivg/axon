"use client";

import { Code } from "@connectrpc/connect";
import { Ban, BadgeCheck, KeyRound, Link2, Link2Off, LogOut, Pencil, RotateCcw, ShieldCheck, UserX } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { AuditList } from "@/components/admin/audit-table";
import { Mono, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { CopyID, Fact, Facts, RecordEmpty, RecordFrame, RecordLoading, RecordNote, RecordPage, RecordRow, RecordRows, RecordSection, RecordState } from "@/components/admin/record";
import { ProfileDialog, RolesDialog } from "@/components/admin/users/user-dialogs";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { UserStatus, type LinkedAccount } from "@/gen/axon/auth/v1/admin_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { useSession } from "@/lib/auth/session";
import { isCode } from "@/lib/errors";
import { initials, personName } from "@/lib/format";
import { useAdminUser, useRestoreUser, useRevokeUserSessions, useSuspendUser, useUnlinkAccount } from "@/lib/query/admin";
import { useAdminRoles } from "@/lib/query/roles";
import { cn } from "@/lib/utils";

enum Pending {
  None = "",
  Suspend = "suspend",
  Restore = "restore",
  Sessions = "sessions",
  Roles = "roles",
  Profile = "profile",
  Unlink = "unlink",
}

enum Activity {
  On = "on",
  By = "by",
}

const back = { section: "Users", back: "/users", backLabel: "Back to users" };

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
  const [activity, setActivity] = useState<Activity>(Activity.On);

  if (detail.isPending) return <RecordLoading {...back} label="Loading account" />;

  if (detail.isError || !detail.data.user?.user) {
    return (
      <RecordState {...back}>
        {isCode(detail.error, Code.NotFound) ? <EmptyState icon={UserX} title="This account does not exist" /> : <ErrorCard title="Could not load the account" error={detail.error} onRetry={() => void detail.refetch()} />}
      </RecordState>
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
  const auditHref = activity === Activity.On ? `/audit?f.target_type=user&f.target=${user.id}` : `/audit?f.actor=${user.id}`;

  return (
    <RecordPage
      {...back}
      media={<Avatar src={user.avatarUrls?.medium || user.avatarUrl} alt="" fallback={initials(name)} sizes="64px" className={cn("size-16 rounded-2xl text-lg", suspended && "grayscale")} />}
      title={name}
      lines={
        <>
          {user.username ? <span>@{user.username}</span> : null}
          <span className="inline-flex min-w-0 items-center gap-1.5">
            <span className="break-all">{user.email}</span>
            {user.emailVerified ? <BadgeCheck aria-label="verified" className="size-4 shrink-0 text-success" /> : <span className="text-xs text-warning">unverified</span>}
          </span>
        </>
      }
      tags={suspended || self ? (
        <>
          {suspended ? <Badge variant="destructive" dot>Suspended</Badge> : null}
          {self ? <Badge variant="signal">You</Badge> : null}
        </>
      ) : undefined}
      actions={
        <>
          {access.can(Permission.UsersMutate) ? <Button variant="outline" size="sm" onClick={() => setPending(Pending.Profile)}><Pencil aria-hidden /> Edit name</Button> : null}
          {mutate ? (
            <>
              <Button variant="outline" size="sm" disabled={sessions.length === 0} onClick={() => setPending(Pending.Sessions)}><LogOut aria-hidden /> Sign out everywhere</Button>
              {suspended ? (
                <Button variant="outline" size="sm" onClick={() => setPending(Pending.Restore)}><RotateCcw aria-hidden /> Restore</Button>
              ) : (
                <Button variant="destructive-outline" size="sm" onClick={() => setPending(Pending.Suspend)}><Ban aria-hidden /> Suspend</Button>
              )}
            </>
          ) : null}
        </>
      }
      facts={
        <Facts>
          <Fact term="Status">{suspended ? <span className="text-destructive">Suspended</span> : "Active"}</Fact>
          <Fact term="Joined"><When at={user.createdAt} /></Fact>
          <Fact term="Last seen"><When at={admin.lastSeenAt} /></Fact>
          <Fact term="Sessions"><span className="tabular-nums">{sessions.length}</span></Fact>
          <Fact term="Password">{detail.data.hasPassword ? "Set" : <span className="text-muted-foreground">Not set</span>}</Fact>
          <Fact term="User ID"><CopyID value={user.id} label="user ID" /></Fact>
        </Facts>
      }
    >
      {suspended ? (
        <RecordNote tone="destructive">
          <dl className="flex flex-col gap-1">
            <div className="flex flex-wrap items-baseline gap-x-2">
              <dt className="font-medium text-destructive">Suspended</dt>
              <dd className="text-xs text-muted-foreground"><When at={admin.suspendedAt} /></dd>
            </div>
            <div>
              <dt className="sr-only">Reason</dt>
              <dd className="break-words">{admin.suspendedReason}</dd>
            </div>
          </dl>
        </RecordNote>
      ) : null}

      <RecordSection
        id="roles"
        title="Roles"
        count={user.roles.length}
        aside={editRoles ? <Button variant="outline" size="sm" onClick={() => setPending(Pending.Roles)}><KeyRound aria-hidden /> Edit roles</Button> : undefined}
      >
        <RecordFrame>
          {user.roles.length === 0 ? <RecordEmpty>No roles granted</RecordEmpty> : (
            <RecordRows label="Granted roles">
              {user.roles.map((roleName) => {
                const role = roles.data?.find((candidate) => candidate.name === roleName);
                const body = (
                  <>
                    <span aria-hidden className={cn("grid size-8 shrink-0 place-items-center rounded-lg", roleName === "admin" ? "bg-signal/15 text-signal" : "bg-muted text-muted-foreground")}>
                      <ShieldCheck className="size-4" />
                    </span>
                    <span className="flex min-w-0 flex-col">
                      <span className="truncate text-sm font-medium">{role?.title || roleName}</span>
                      <Mono className="truncate">{roleName}</Mono>
                    </span>
                  </>
                );
                return (
                  <RecordRow key={roleName} aside={role ? <span className="text-xs text-muted-foreground tabular-nums">{role.permissions.length} permissions</span> : undefined}>
                    {role && access.can(Permission.RolesRead) ? <Link href={`/roles/${role.id}`} className="flex min-w-0 items-center gap-3 hover:underline">{body}</Link> : body}
                  </RecordRow>
                );
              })}
            </RecordRows>
          )}
        </RecordFrame>
      </RecordSection>

      <RecordSection id="sign-in" title="Sign-in methods">
        <RecordFrame>
          <RecordRows label="Sign-in methods">
            <RecordRow aside={detail.data.hasPassword ? <Badge variant="success" dot>Set</Badge> : <Badge>Not set</Badge>}>
              <span aria-hidden className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground"><KeyRound className="size-4" /></span>
              <span className="text-sm font-medium">Password</span>
            </RecordRow>
            {linked.map((item) => (
              <RecordRow
                key={item.provider}
                aside={mutate ? (
                  <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={() => {
                    setAccount(item);
                    setPending(Pending.Unlink);
                  }}><Link2Off aria-hidden /> Unlink</Button>
                ) : undefined}
              >
                <span aria-hidden className="grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-muted-foreground"><Link2 className="size-4" /></span>
                <span className="flex min-w-0 flex-col">
                  <span className="text-sm font-medium capitalize">{item.provider}</span>
                  <span className="truncate text-xs text-muted-foreground">{item.email || "no email"}, linked <When at={item.linkedAt} /></span>
                </span>
              </RecordRow>
            ))}
          </RecordRows>
        </RecordFrame>
      </RecordSection>

      <RecordSection id="sessions" title="Active sessions" count={sessions.length}>
        <RecordFrame>
          {sessions.length === 0 ? <RecordEmpty>Not signed in anywhere</RecordEmpty> : (
            <ScrollArea orientation="horizontal">
              <table aria-label="Active sessions" className="w-full min-w-[36rem] text-left text-sm">
                <thead className="bg-surface-raised text-xs text-muted-foreground">
                  <tr>
                    <th scope="col" className="px-4 py-2.5 font-medium">Device</th>
                    <th scope="col" className="px-4 py-2.5 font-medium">IP</th>
                    <th scope="col" className="px-4 py-2.5 font-medium">Started</th>
                    <th scope="col" className="px-4 py-2.5 font-medium">Last used</th>
                  </tr>
                </thead>
                <tbody>
                  {sessions.map((session) => (
                    <tr key={session.id} className="border-t border-line">
                      <td className="max-w-[22rem] px-4 py-3" title={session.userAgent}>
                        <span className="flex min-w-0 items-center gap-2">
                          <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", session.online ? "bg-success" : "bg-muted-foreground/40")} />
                          <span className="truncate">{session.userAgent || "Unknown device"}</span>
                          {session.online ? <span className="sr-only">online</span> : null}
                        </span>
                      </td>
                      <td className="px-4 py-3"><Mono>{session.ip || "—"}</Mono></td>
                      <td className="px-4 py-3 text-muted-foreground"><When at={session.startedAt} /></td>
                      <td className="px-4 py-3 text-muted-foreground"><When at={session.lastUsedAt} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </ScrollArea>
          )}
        </RecordFrame>
      </RecordSection>

      {access.can(Permission.AuditRead) ? (
        <RecordSection id="history" title="Activity" aside={<Link className={buttonVariants({ variant: "link", size: "sm" })} href={auditHref}>Open in audit log</Link>}>
          <Tabs value={activity} onValueChange={(value) => setActivity(value as Activity)} className="flex flex-col gap-4">
            <TabsList className="self-start">
              <TabsTrigger value={Activity.On}>On this account</TabsTrigger>
              <TabsTrigger value={Activity.By}>By this account</TabsTrigger>
            </TabsList>
            <TabsContent value={Activity.On}><AuditList filters={{ targetType: "user", target: user.id }} /></TabsContent>
            <TabsContent value={Activity.By}><AuditList filters={{ actor: user.id }} showTarget /></TabsContent>
          </Tabs>
        </RecordSection>
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
    </RecordPage>
  );
}
