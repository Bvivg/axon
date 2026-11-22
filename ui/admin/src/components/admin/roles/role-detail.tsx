"use client";

import { Code } from "@connectrpc/connect";
import { KeyRound, Pencil, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";

import { AuditList } from "@/components/admin/audit-table";
import { Mono, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { RoleForm } from "@/components/admin/roles/role-form";
import { roleHref, RoleKind } from "@/components/admin/roles/roles-list";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { Pane, PaneSection } from "@/components/ui/pane";
import { Skeleton } from "@/components/ui/skeleton";
import type { AdminRole } from "@/gen/axon/auth/v1/admin_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { isCode } from "@/lib/errors";
import { groupPermissions, useAdminRole, useDeleteRole, usePermissionCatalog } from "@/lib/query/roles";

function Fact({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{term}</dt>
      <dd className="min-w-0 text-sm break-words">{children}</dd>
    </div>
  );
}

function RoleShell({ title, back, children, actions }: { title: string; back: string; children: ReactNode; actions?: ReactNode }) {
  return <Pane title={title} back={back} backLabel="Back" width="wide" actions={actions}>{children}</Pane>;
}

function useLoadedRole(id: string, title: string, back: string) {
  const role = useAdminRole(id);
  if (role.isPending) {
    return { view: <RoleShell title={title} back={back}><div role="status" aria-label="Loading role" className="space-y-3"><Skeleton className="h-28 w-full" /><Skeleton className="h-40 w-full" /></div></RoleShell> };
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
  if (!catalog.data) return <Skeleton className="h-24 w-full" />;
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      {groupPermissions(catalog.data).map(([namespace, permissions]) => (
        <div key={namespace} className="flex flex-col gap-1.5 rounded-lg border border-line p-3">
          <span className="font-mono text-xs text-muted-foreground">{namespace}</span>
          <ul aria-label={`${namespace} permissions`} className="flex flex-wrap gap-1.5">
            {permissions.map((permission) => (
              <li key={permission.name}>
                <Badge variant={granted.has(permission.name) ? "success" : "outline"} className={granted.has(permission.name) ? undefined : "text-muted-foreground line-through decoration-muted-foreground/50"}>
                  {permission.action}
                </Badge>
              </li>
            ))}
          </ul>
        </div>
      ))}
      {unknown.length ? <p className="text-xs text-muted-foreground">Not in the catalog: {unknown.join(", ")}</p> : null}
    </div>
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
    <RoleShell
      title={role.title || role.name}
      back="/roles"
      actions={mutate ? (
        <>
          <Link href={`${roleHref(role.id)}/edit`} className={buttonVariants({ variant: "outline", size: "sm" })}><Pencil aria-hidden /> Edit</Link>
          <Button variant="destructive-outline" size="sm" disabled={members > 0} title={members > 0 ? "Take the role away from its members first" : undefined} onClick={() => setDeleting(true)}>
            <Trash2 aria-hidden /> Delete
          </Button>
        </>
      ) : undefined}
    >
      <PaneSection id="role" title="Role" aside={<RoleKind role={role} />}>
        <dl className="grid grid-cols-[repeat(auto-fill,minmax(12rem,1fr))] gap-x-6 gap-y-3">
          <Fact term="Name"><Mono>{role.name}</Mono></Fact>
          <Fact term="Title">{role.title || "—"}</Fact>
          <Fact term="Members"><Link href={`/users?role=${encodeURIComponent(role.name)}`} className="hover:underline">{members.toLocaleString()}</Link></Fact>
          <Fact term="Created"><When at={role.createTime} /></Fact>
          <Fact term="Updated"><When at={role.updateTime} /></Fact>
          <Fact term="Version"><Mono>{role.etag}</Mono></Fact>
        </dl>
        {role.system ? <p className="text-xs text-muted-foreground">Built-in roles are managed by migrations and cannot be edited here.</p> : null}
      </PaneSection>

      <PaneSection id="permissions" title={`Permissions · ${role.permissions.length}`}>
        <Permissions role={role} />
      </PaneSection>

      {access.can(Permission.AuditRead) ? (
        <PaneSection id="history" title="History" aside={<Link className={buttonVariants({ variant: "link", size: "sm" })} href={`/audit?targetType=role&target=${role.id}`}>Open in audit log</Link>}>
          <AuditList filters={{ targetType: "role", target: role.id }} />
        </PaneSection>
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
    </RoleShell>
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
