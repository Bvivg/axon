"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { KeyRound, Plus } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";

import { AdminListPage, count, Mono, When } from "@/components/admin/admin-ui";
import { columnFilters, columnSorts, DataTable, MobileSlot, type TableList } from "@/components/admin/data-table";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import type { AdminRole } from "@/gen/axon/auth/v1/admin_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { ListMode, useListState } from "@/lib/admin/list";
import { FilterKind, localRows, type LocalValue } from "@/lib/admin/list-query";
import { fromTimestamp } from "@/lib/format";
import { useAdminRoles } from "@/lib/query/roles";

export function roleHref(id: string): string {
  return `/roles/${id}`;
}

export function RoleKind({ role }: { role: AdminRole }) {
  return role.system ? <Badge variant="signal">Built-in</Badge> : <Badge>Custom</Badge>;
}

const kindOptions = [
  { value: "builtin", label: "Built-in" },
  { value: "custom", label: "Custom" },
];

const columns: ColumnDef<AdminRole, unknown>[] = [
  {
    id: "name",
    header: "Role",
    meta: { label: "Name", sort: "name", filter: { field: "name", label: "Name", kind: FilterKind.Text }, mobile: MobileSlot.Title },
    cell: ({ row }) => (
      <Link href={roleHref(row.original.id)} className="flex min-w-0 flex-col">
        <span className="truncate font-medium">{row.original.title || row.original.name}</span>
        <Mono>{row.original.name}</Mono>
      </Link>
    ),
  },
  {
    id: "kind",
    header: "Type",
    meta: { filter: { field: "kind", label: "Type", kind: FilterKind.Choice, options: kindOptions }, mobile: MobileSlot.Subtitle },
    cell: ({ row }) => <RoleKind role={row.original} />,
  },
  {
    id: "permissions",
    header: "Permissions",
    meta: { sort: "permissions", filter: { field: "permissions", label: "Permissions", kind: FilterKind.Number }, align: "end" },
    cell: ({ row }) => <span className="tabular-nums">{row.original.permissions.length}</span>,
  },
  {
    id: "members",
    header: "Members",
    meta: { sort: "members", filter: { field: "members", label: "Members", kind: FilterKind.Number }, align: "end" },
    cell: ({ row }) => (
      <Link href={`/users?f.role=${encodeURIComponent(row.original.name)}`} className="tabular-nums hover:underline">
        {Number(row.original.memberCount).toLocaleString()}
      </Link>
    ),
  },
  {
    id: "updated",
    header: "Updated",
    meta: { sort: "updated", filter: { field: "updated", label: "Updated", kind: FilterKind.Date } },
    cell: ({ row }) => <When at={row.original.updateTime} />,
  },
];

const defs = columnFilters(columns);
const sorts = columnSorts(columns);

function read(role: AdminRole, field: string): LocalValue {
  switch (field) {
    case "name":
      return `${role.title} ${role.name}`;
    case "kind":
      return role.system ? "builtin" : "custom";
    case "permissions":
      return role.permissions.length;
    case "members":
      return Number(role.memberCount);
    case "updated":
      return fromTimestamp(role.updateTime) ?? undefined;
  }
}

export function RolesList() {
  const access = useAdminAccess();
  const roles = useAdminRoles();
  const state = useListState(defs);
  const rows = useMemo(() => localRows(roles.data ?? [], defs, state.values, state.sort, read), [roles.data, state.values, state.sort]);
  const list: TableList<AdminRole> = { mode: ListMode.Infinite, rows, total: rows.length, status: roles.status, error: roles.error, refetch: roles.refetch };
  return (
    <AdminListPage
      title="Roles"
      subtitle={roles.data ? count(roles.data.length, "role") : undefined}
      actions={access.can(Permission.RolesMutate) ? (
        <Link href="/roles/new" className={buttonVariants({ size: "sm", className: "pointer-coarse:h-11" })}><Plus aria-hidden /> New role</Link>
      ) : undefined}
      toolbar={<ListToolbar state={state} sorts={sorts} />}
    >
      <DataTable
        label="Roles"
        columns={columns}
        list={list}
        state={state}
        rowKey={(role) => role.id}
        rowHref={(role) => roleHref(role.id)}
        empty={{ icon: KeyRound, title: "No roles defined" }}
      />
    </AdminListPage>
  );
}
