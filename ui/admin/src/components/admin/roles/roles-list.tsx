"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { KeyRound, Plus } from "lucide-react";
import Link from "next/link";

import { AdminListPage, count, Mono, When } from "@/components/admin/admin-ui";
import { DataTable } from "@/components/admin/data-table";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import type { AdminRole } from "@/gen/axon/auth/v1/admin_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { useAdminRoles } from "@/lib/query/roles";

export function roleHref(id: string): string {
  return `/roles/${id}`;
}

export function RoleKind({ role }: { role: AdminRole }) {
  return role.system ? <Badge variant="signal">Built-in</Badge> : <Badge>Custom</Badge>;
}

const columns: ColumnDef<AdminRole, unknown>[] = [
  {
    id: "name",
    header: "Role",
    cell: ({ row }) => (
      <Link href={roleHref(row.original.id)} className="flex min-w-0 flex-col">
        <span className="truncate font-medium">{row.original.title || row.original.name}</span>
        <Mono>{row.original.name}</Mono>
      </Link>
    ),
  },
  { id: "kind", header: "Type", cell: ({ row }) => <RoleKind role={row.original} /> },
  { id: "permissions", header: "Permissions", cell: ({ row }) => <span className="tabular-nums">{row.original.permissions.length}</span> },
  {
    id: "members",
    header: "Members",
    cell: ({ row }) => (
      <Link href={`/users?role=${encodeURIComponent(row.original.name)}`} className="tabular-nums hover:underline">
        {Number(row.original.memberCount).toLocaleString()}
      </Link>
    ),
  },
  { id: "updated", header: "Updated", cell: ({ row }) => <When at={row.original.updateTime} /> },
];

export function RolesList() {
  const access = useAdminAccess();
  const roles = useAdminRoles();
  const rows = roles.data ?? [];
  return (
    <AdminListPage
      title="Roles"
      subtitle={roles.data ? count(BigInt(roles.data.length), "role") : undefined}
      actions={access.can(Permission.RolesMutate) ? (
        <Link href="/roles/new" className={buttonVariants({ size: "sm" })}><Plus aria-hidden /> New role</Link>
      ) : undefined}
    >
      <DataTable
        label="Roles"
        columns={columns}
        rows={rows}
        rowKey={(role) => role.id}
        rowHref={(role) => roleHref(role.id)}
        status={roles.status}
        error={roles.error}
        onRetry={() => void roles.refetch()}
        filtered={false}
        empty={{ icon: KeyRound, title: "No roles defined" }}
      />
    </AdminListPage>
  );
}
