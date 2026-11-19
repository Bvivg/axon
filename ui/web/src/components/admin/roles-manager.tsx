"use client";

import { Search } from "lucide-react";
import Link from "next/link";
import { useState, type FormEvent } from "react";

import { WorkspacePage } from "@/components/nav/workspace-page";
import { Avatar } from "@/components/ui/avatar";
import { Alert } from "@/components/ui/alert";
import { AlertDialog, AlertDialogClose, AlertDialogContent, AlertDialogDescription, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { User } from "@/gen/axon/auth/v1/auth_pb";
import { Code } from "@connectrpc/connect";
import { calendarDate, fromTimestamp, initials, personName } from "@/lib/format";
import { describe, isCode } from "@/lib/errors";
import { useAssignRole, useLookupUser, useRevokeRole, useRoles } from "@/lib/query/admin";

export function RolesManager() {
  const roles = useRoles();
  const lookupUser = useLookupUser();
  const assignRole = useAssignRole();
  const revokeRole = useRevokeRole();

  const [email, setEmail] = useState("");
  const [found, setFound] = useState<User | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [feedback, setFeedback] = useState("");
  const [change, setChange] = useState<{ id: string; name: string; granted: boolean }>();
  const changing = assignRole.isPending || revokeRole.isPending;

  async function lookup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setFeedback("");
    try {
      const user = await lookupUser.mutateAsync(email.trim());
      setFound(user);
    } catch (err) {
      setFound(null);
      setError(isCode(err, Code.NotFound) ? "No account found for that email." : describe(err));
    }
  }

  async function toggleRole(roleId: string, roleName: string, granted: boolean) {
    if (!found) {
      return;
    }
    setError(null);
    setFeedback("");
    try {
      if (granted) {
        await revokeRole.mutateAsync({ userId: found.id, roleId });
      } else {
        await assignRole.mutateAsync({ userId: found.id, roleId });
      }
      setFound(previous => previous?.id === found.id ? {
        ...previous, roles: granted ? previous.roles.filter(role => role !== roleName) : [...previous.roles, roleName],
      } : previous);
      setFeedback(granted ? `Removed ${roleName} from ${found.displayName || found.email}.` : `Assigned ${roleName} to ${found.displayName || found.email}.`);
      setChange(undefined);
      try {
        const updated = await lookupUser.mutateAsync(found.email);
        setFound(previous => previous?.id === found.id ? updated : previous);
      } catch {
        setError("The change was saved. Look up the account again to refresh its roles.");
      }
    } catch (err) {
      setError(describe(err));
    }
  }

  if (roles.isError) {
    return (
      <WorkspacePage title="Users and roles" back="/profile" backLabel="Back to profile">
        <Alert>{describe(roles.error)} <Button variant="ghost" onClick={() => void roles.refetch()}>Try again</Button></Alert>
      </WorkspacePage>
    );
  }

  const joined = fromTimestamp(found?.createdAt);
  const name = found ? personName(found) : "";

  return (
    <WorkspacePage title="Users and roles" back="/profile" backLabel="Back to profile" actions={
      <nav aria-label="Operations" className="workspace-mobile-only flex-wrap gap-2">
        <Link href="/admin/service-map" className={buttonVariants({ variant: "outline", size: "sm" })}>Service map</Link>
        <Link href="/admin/gateway" className={buttonVariants({ variant: "outline", size: "sm" })}>Gateway</Link>
      </nav>
    }>
      <Card className="min-w-0 gap-0 overflow-hidden rounded-[15px] py-0">
        <CardHeader className="gap-1 border-b border-border px-5 py-4">
          <CardTitle>Accounts</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 p-0">
          <form className="flex flex-wrap items-end gap-3 border-b border-border p-4" onSubmit={(e) => void lookup(e)}>
            <div className="min-w-0 flex-[1_1_13rem] space-y-1.5">
              <Label htmlFor="lookup-email" className="sr-only">Email</Label>
              <Input id="lookup-email" type="email" className="min-h-11 rounded-[10px]" placeholder="Account email" required value={email} onChange={(e) => setEmail(e.target.value)} />
            </div>
            <Button className="min-h-11" type="submit" disabled={lookupUser.isPending || changing || !!change}>
              <Search aria-hidden /> {lookupUser.isPending ? "Looking up…" : "Look up"}
            </Button>
          </form>

          {error && !change ? <Alert className="mx-4">{error}</Alert> : null}
          <div role="status">{feedback ? <p className="mx-4 rounded-lg border border-success/40 bg-success/10 px-3 py-2 text-sm font-medium">{feedback}</p> : null}</div>

          {found ? (
            <div>
              <div className="overflow-x-auto">
                <table aria-label="Matching account" className="w-full min-w-[30rem] border-collapse text-left">
                  <thead className="border-y border-border bg-muted/50 text-xs text-muted-foreground">
                    <tr><th scope="col" className="px-4 py-3">Account</th><th scope="col" className="px-4 py-3">Email</th><th scope="col" className="px-4 py-3">Roles</th><th scope="col" className="px-4 py-3">Joined</th></tr>
                  </thead>
                  <tbody>
                    <tr className="border-b border-border text-xs">
                      <th scope="row" className="px-4 py-3 font-medium">
                        <span className="flex items-center gap-2"><Avatar src={found.avatarUrls?.small || found.avatarUrl} alt="" fallback={initials(name)} sizes="36px" className="size-9 rounded-[11px]" /> {name}</span>
                      </th>
                      <td className="px-4 py-3 [overflow-wrap:anywhere]">{found.email}</td>
                      <td className="px-4 py-3"><div className="flex flex-wrap gap-1.5">{found.roles.map((role) => <Badge key={role} variant="secondary">{role}</Badge>)}</div></td>
                      <td className="px-4 py-3 whitespace-nowrap">{joined ? <time dateTime={joined.toISOString()}>{calendarDate(joined)}</time> : "—"}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <section className="p-4" aria-labelledby="role-assignments">
                <h2 id="role-assignments" className="text-sm font-semibold">Role assignments</h2>
                <table aria-label="Role assignments" className="mt-2 w-full border-collapse text-left">
                  <thead className="border-b border-border text-xs text-muted-foreground"><tr><th scope="col" className="py-2">Role</th><th scope="col" className="py-2 text-right">Access</th></tr></thead>
                  <tbody className="divide-y divide-border">
                    {(roles.data ?? []).map((role) => {
                      const granted = found.roles.includes(role.name);
                      return (
                        <tr key={role.id}>
                          <th scope="row" className="py-2 pr-2 text-sm font-medium [overflow-wrap:anywhere]">{role.name}</th>
                          <td className="py-2 text-right"><Button
                            type="button"
                            variant={granted ? "outline" : "default"}
                            size="sm"
                            className="min-h-11"
                            disabled={lookupUser.isPending || assignRole.isPending || revokeRole.isPending}
                            aria-label={`${granted ? "Revoke" : "Assign"} ${role.name}`}
                            onClick={() => { setError(null); setFeedback(""); setChange({ id: role.id, name: role.name, granted }); }}
                          >
                            {granted ? "Revoke" : "Assign"}
                          </Button></td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </section>
            </div>
          ) : !lookupUser.isPending && !error ? <p className="px-4 pb-5 text-sm text-muted-foreground">Enter an email to see its account and assigned roles.</p> : null}
        </CardContent>
      </Card>
      <Card id="permissions" className="min-w-0 rounded-[15px]">
        <CardHeader className="flex items-center justify-between gap-2"><CardTitle>Roles and permissions</CardTitle>{roles.data ? <Badge variant="secondary">{roles.data.length} roles</Badge> : null}</CardHeader>
        <CardContent>
          {roles.isPending ? <p role="status" className="text-sm text-muted-foreground">Loading roles…</p> : null}
          {roles.isSuccess && !roles.data.length ? <p className="text-sm text-muted-foreground">No roles configured.</p> : null}
          <ul className="divide-y divide-border">
            {roles.data?.map((role) => (
              <li key={role.id} className="grid gap-2 py-3 sm:grid-cols-[minmax(5rem,1fr)_3fr]">
                <h2 className="text-sm font-semibold [overflow-wrap:anywhere]">{role.name}</h2>
                {role.permissions.length ? (
                  <ul aria-label={`Permissions for ${role.name}`} className="flex flex-wrap gap-1.5">
                    {role.permissions.map((permission) => <li key={permission.id}><Badge variant="secondary">{permission.namespace}.{permission.action}</Badge></li>)}
                  </ul>
                ) : <p className="text-sm text-muted-foreground">No permissions</p>}
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
      <AlertDialog open={!!change} onOpenChange={open => { if (!open && !changing) setChange(undefined); }}>
        <AlertDialogContent className="[overflow-wrap:anywhere]">
          <AlertDialogTitle>{change?.granted ? "Revoke role?" : "Assign role?"}</AlertDialogTitle>
          <AlertDialogDescription>{change?.name} · {found?.displayName || found?.email}</AlertDialogDescription>
          {error ? <Alert className="mt-3">{error}</Alert> : null}
          <div className="mt-4 flex justify-end gap-2">
            <AlertDialogClose render={<Button variant="outline" className="min-h-11" disabled={changing} />}>Cancel</AlertDialogClose>
            <Button className="min-h-11" variant={change?.granted ? "destructive" : "default"} disabled={changing} onClick={() => {
              if (change) void toggleRole(change.id, change.name, change.granted);
            }}>{changing ? "Saving…" : change?.granted ? "Revoke role" : "Assign role"}</Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </WorkspacePage>
  );
}
