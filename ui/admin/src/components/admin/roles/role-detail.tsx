"use client";

import { Code } from "@connectrpc/connect";
import { Check, KeyRound, Pencil, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";

import { AuditList } from "@/components/admin/audit-table";
import { Mono, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { CopyID, Fact, Facts, RecordFrame, RecordIcon, RecordLoading, RecordNote, RecordPage, RecordSection } from "@/components/admin/record";
import { RoleForm } from "@/components/admin/roles/role-form";
import { roleHref, RoleKind } from "@/components/admin/roles/roles-list";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { Pane } from "@/components/ui/pane";
import { Skeleton } from "@/components/ui/skeleton";
import type { AdminRole } from "@/gen/axon/auth/v1/admin_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { isCode } from "@/lib/errors";
import { cn } from "@/lib/utils";
import { groupPermissions, useAdminRole, useDeleteRole, usePermissionCatalog } from "@/lib/query/roles";

function RoleShell({ title, back, children, actions }: { title: string; back: string; children: ReactNode; actions?: ReactNode }) {
  return <Pane title={title} back={back} backLabel="Back" width="wide" actions={actions}>{children}</Pane>;
}

function useLoadedRole(id: string, title: string, back: string) {
  const role = useAdminRole(id);
  if (role.isPending) {
    return { view: <RecordLoading section={title} back={back} backLabel="Back" label="Loading role" /> };
  }
  if (role.isError) {
    return {
      view: (
        <RoleShell title={title} back={back}>
          {isCode(role.error, Code.NotFound) ? <EmptyState icon={KeyRound} title="This role does not exist" /> : <ErrorCard title="Could not load the role" error={role.error} onRetry={() => void role.refetch()} />}
        </RoleShell>
      ),
    };
  }
  return { role: role.data };
}

function Permissions({ role }: { role: AdminRole }) {
  const catalog = usePermissionCatalog();
  const granted = new Set(role.permissions);
  const known = new Set(catalog.data?.map((permission) => permission.name));
  const unknown = role.permissions.filter((name) => catalog.data && !known.has(name));
  if (!catalog.data) return <Skeleton className="h-40 w-full rounded-xl" />;
  return (
    <RecordFrame>
      <ul className="divide-y divide-line">
        {groupPermissions(catalog.data).map(([namespace, permissions]) => {
          const have = permissions.filter((permission) => granted.has(permission.name)).length;
          return (
            <li key={namespace} className="grid gap-x-6 gap-y-2 px-4 py-3.5 sm:grid-cols-[9rem_minmax(0,1fr)]">
              <div className="flex items-baseline justify-between gap-2 sm:flex-col sm:justify-start sm:gap-0.5">
                <span className={cn("font-mono text-[13px]", have === 0 && "text-muted-foreground")}>{namespace}</span>
                <span className="text-xs text-muted-foreground tabular-nums">{have} of {permissions.length}</span>
              </div>
              <ul aria-label={`${namespace} permissions`} className="flex flex-wrap gap-1.5">
                {permissions.map((permission) => {
                  const on = granted.has(permission.name);
                  return (
                    <li
                      key={permission.name}
                      title={permission.name}
                      className={cn(
                        "inline-flex h-7 items-center gap-1 rounded-md border px-2 text-xs",
                        on ? "border-success/30 bg-success/10 font-medium" : "border-dashed border-line text-muted-foreground",
                      )}
                    >
                      {on ? <Check aria-hidden className="size-3.5 text-success" /> : null}
                      {permission.action}
                      {on ? null : <span className="sr-only">(not granted)</span>}
                    </li>
                  );
                })}
              </ul>
            </li>
          );
        })}
      </ul>
      {unknown.length ? <p className="border-t border-line px-4 py-3 text-xs text-muted-foreground">Not in the catalog: <span className="font-mono">{unknown.join(", ")}</span></p> : null}
    </RecordFrame>
  );
}

export function RoleDetail({ id }: { id: string }) {
  const access = useAdminAccess();
  const router = useRouter();
  const remove = useDeleteRole();
  const [deleting, setDeleting] = useState(false);
  const loaded = useLoadedRole(id, "Role", "/roles");
  if (!loaded.role) return loaded.view;
  const role = loaded.role;
  const members = Number(role.memberCount);
  const mutate = access.can(Permission.RolesMutate) && !role.system;

  return (
    <RecordPage
      section="Roles"
      back="/roles"
      backLabel="Back to roles"
      media={<RecordIcon icon={KeyRound} tone={role.system ? "signal" : "muted"} />}
      title={role.title || role.name}
      lines={<Mono className="text-sm">{role.name}</Mono>}
      tags={<RoleKind role={role} />}
      actions={mutate ? (
        <>
          <Link href={`${roleHref(role.id)}/edit`} className={buttonVariants({ variant: "outline", size: "sm" })}><Pencil aria-hidden /> Edit</Link>
          <Button variant="destructive-outline" size="sm" disabled={members > 0} title={members > 0 ? "Take the role away from its members first" : undefined} onClick={() => setDeleting(true)}>
            <Trash2 aria-hidden /> Delete
          </Button>
        </>
      ) : undefined}
      facts={
        <Facts>
          <Fact term="Members"><Link href={`/users?f.role=${encodeURIComponent(role.name)}`} className="tabular-nums hover:underline">{members.toLocaleString()}</Link></Fact>
          <Fact term="Permissions"><span className="tabular-nums">{role.permissions.length}</span></Fact>
          <Fact term="Created"><When at={role.createTime} /></Fact>
          <Fact term="Updated"><When at={role.updateTime} /></Fact>
          <Fact term="Role ID"><CopyID value={role.id} label="role ID" /></Fact>
        </Facts>
      }
    >
      {role.system ? <RecordNote>Built-in roles are managed by migrations and cannot be edited here.</RecordNote> : null}

      <RecordSection id="permissions" title="Permissions" count={role.permissions.length}>
        <Permissions role={role} />
      </RecordSection>

      {access.can(Permission.AuditRead) ? (
        <RecordSection id="history" title="History" aside={<Link className={buttonVariants({ variant: "link", size: "sm" })} href={`/audit?f.target_type=role&f.target=${role.id}`}>Open in audit log</Link>}>
          <AuditList filters={{ targetType: "role", target: role.id }} />
        </RecordSection>
      ) : null}

      <ConfirmAction
        open={deleting}
        onOpenChange={setDeleting}
        title={`Delete role ${role.name}?`}
        consequence="The role disappears from the catalog. Its definition stays in the audit log."
        confirmLabel="Delete role"
        onConfirm={async (reason) => {
          await remove.mutateAsync({ roleId: role.id, etag: role.etag, reason });
          router.push("/roles");
        }}
      />
    </RecordPage>
  );
}

export function RoleEdit({ id }: { id: string }) {
  const loaded = useLoadedRole(id, "Edit role", roleHref(id));
  if (!loaded.role) return loaded.view;
  const role = loaded.role;
  if (role.system) {
    return <RoleShell title={role.title || role.name} back={roleHref(id)}><EmptyState icon={KeyRound} title="Built-in roles cannot be changed" /></RoleShell>;
  }
  return <RoleShell title={`Edit ${role.title || role.name}`} back={roleHref(id)}><RoleForm role={role} /></RoleShell>;
}

export function RoleCreate() {
  return <RoleShell title="New role" back="/roles"><RoleForm /></RoleShell>;
}
