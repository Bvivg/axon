"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Ban, ExternalLink, RotateCcw, Users } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { AdminListPage, userHref, When } from "@/components/admin/admin-ui";
import { bulkApply } from "@/components/admin/bulk-bar";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { columnFilters, columnSorts, DataTable, MobileSlot } from "@/components/admin/data-table";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem, DropdownMenuLinkItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu";
import { UserStatus, type AdminUser } from "@/gen/axon/auth/v1/admin_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { bulkLimit } from "@/lib/admin/bulk";
import { useAdminList, useListState } from "@/lib/admin/list";
import { FilterKind, type Option } from "@/lib/admin/list-query";
import { useSession } from "@/lib/auth/session";
import { authAdminClient } from "@/lib/connect/clients";
import { initials, personName } from "@/lib/format";
import { adminKeys, loadUsers, useRefreshAfterBulk, useRoles } from "@/lib/query/admin";

enum Pending {
  None = "",
  Suspend = "suspend",
  Restore = "restore",
}

const extra = ["query"] as const;

export function StatusBadge({ user }: { user: AdminUser }) {
  return user.status === UserStatus.SUSPENDED ? <Badge variant="destructive" dot>Suspended</Badge> : <span className="text-muted-foreground">Active</span>;
}

function userColumns(roleOptions: Option[]): ColumnDef<AdminUser, unknown>[] {
  return [
    {
      id: "account",
      header: "Account",
      meta: { label: "Name", sort: "name", filter: { field: "name", label: "Name", kind: FilterKind.Text }, mobile: MobileSlot.Title },
      cell: ({ row }) => {
        const user = row.original.user;
        if (!user) return null;
        const name = personName(user);
        return (
          <Link href={userHref(user.id)} className="flex min-w-0 items-center gap-2.5">
            <Avatar src={user.avatarUrls?.small || user.avatarUrl} alt="" fallback={initials(name)} sizes="28px" className="size-7 rounded-[9px] text-[10px]" />
            <span className="min-w-0">
              <span className="block truncate font-medium">{name}</span>
              {user.username ? <span className="block truncate text-xs text-muted-foreground">@{user.username}</span> : null}
            </span>
          </Link>
        );
      },
    },
    {
      id: "email",
      header: "Email",
      meta: { sort: "email", filter: { field: "email", label: "Email", kind: FilterKind.Text }, mobile: MobileSlot.Subtitle },
      cell: ({ row }) => <span className="break-all">{row.original.user?.email}</span>,
    },
    {
      id: "roles",
      header: "Roles",
      meta: { filter: { field: "role", label: "Role", kind: FilterKind.Choice, options: roleOptions } },
      cell: ({ row }) => row.original.user?.roles.length ? (
        <span className="flex flex-wrap gap-1">
          {row.original.user.roles.map((role) => <Badge key={role} variant={role === "admin" ? "signal" : "outline"}>{role}</Badge>)}
        </span>
      ) : <span className="text-muted-foreground">—</span>,
    },
    {
      id: "status",
      header: "Status",
      meta: { sort: "status", filter: { field: "status", label: "Status", kind: FilterKind.Choice, options: [{ value: "active", label: "Active" }, { value: "suspended", label: "Suspended" }] } },
      cell: ({ row }) => <StatusBadge user={row.original} />,
    },
    {
      id: "sessions",
      header: "Sessions",
      meta: { sort: "sessions", filter: { field: "sessions", label: "Sessions", kind: FilterKind.Number }, align: "end" },
      cell: ({ row }) => <span className="tabular-nums">{row.original.activeSessions}</span>,
    },
    {
      id: "joined",
      header: "Joined",
      meta: { sort: "joined", filter: { field: "joined", label: "Joined", kind: FilterKind.Date } },
      cell: ({ row }) => <When at={row.original.user?.createdAt} />,
    },
    {
      id: "seen",
      header: "Last seen",
      meta: { sort: "last_seen", filter: { field: "last_seen", label: "Last seen", kind: FilterKind.Date } },
      cell: ({ row }) => <When at={row.original.lastSeenAt} />,
    },
  ];
}

const rowKey = (row: AdminUser) => row.user?.id ?? "";

export function UsersTable() {
  const access = useAdminAccess();
  const roles = useRoles(access.can(Permission.RolesRead));
  const roleOptions = useMemo(() => (roles.data ?? []).map((role) => ({ value: role.name, label: role.name })), [roles.data]);
  const columns = useMemo(() => userColumns(roleOptions), [roleOptions]);
  const defs = useMemo(() => columnFilters(columns), [columns]);
  const sorts = useMemo(() => columnSorts(columns), [columns]);
  const state = useListState(defs, extra);
  const query = state.state.query ?? "";
  const list = useAdminList([...adminKeys.users, "list", query], state, (request, signal) => loadUsers(request, query, signal), rowKey);
  const { user: me } = useSession();
  const refresh = useRefreshAfterBulk([adminKeys.users]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulk, setBulk] = useState(Pending.None);
  const [single, setSingle] = useState<{ action: Pending; user: AdminUser }>();
  const mutate = access.can(Permission.UsersMutate);
  const picked = list.rows.filter((row) => row.user && selected.has(row.user.id));
  const active = picked.filter((row) => row.status !== UserStatus.SUSPENDED);
  const suspended = picked.filter((row) => row.status === UserStatus.SUSPENDED);
  const singleName = single?.user.user ? personName(single.user.user) : "this account";

  return (
    <AdminListPage
      title="Users"
      subtitle={list.total !== undefined ? `${list.total.toLocaleString()} account${list.total === 1 ? "" : "s"}` : undefined}
      toolbar={<ListToolbar state={state} search={{ label: "Search by name, username or email", placeholder: "Name, username or email", key: "query" }} sorts={sorts} />}
    >
      <DataTable
        label="Users"
        columns={columns}
        list={list}
        state={state}
        rowKey={rowKey}
        rowHref={(row) => userHref(row.user?.id ?? "")}
        empty={{ icon: Users, title: "No accounts yet" }}
        selection={mutate ? { selected, onChange: setSelected, selectable: (row) => !!row.user && row.user.id !== me?.id, limit: bulkLimit } : undefined}
        bulkActions={
          <>
            <Button variant="destructive-outline" size="sm" className="pointer-coarse:h-11" disabled={active.length === 0} onClick={() => setBulk(Pending.Suspend)}><Ban aria-hidden /> Suspend {active.length}</Button>
            <Button variant="outline" size="sm" className="pointer-coarse:h-11" disabled={suspended.length === 0} onClick={() => setBulk(Pending.Restore)}><RotateCcw aria-hidden /> Restore {suspended.length}</Button>
          </>
        }
        rowActions={(row) => (
          <>
            <DropdownMenuLinkItem render={<Link href={userHref(row.user?.id ?? "")} />}><ExternalLink aria-hidden /> Open</DropdownMenuLinkItem>
            {mutate && row.user?.id !== me?.id ? (
              <>
                <DropdownMenuSeparator />
                {row.status === UserStatus.SUSPENDED ? (
                  <DropdownMenuItem onClick={() => setSingle({ action: Pending.Restore, user: row })}><RotateCcw aria-hidden /> Restore</DropdownMenuItem>
                ) : (
                  <DropdownMenuItem className="text-destructive" onClick={() => setSingle({ action: Pending.Suspend, user: row })}><Ban aria-hidden /> Suspend</DropdownMenuItem>
                )}
              </>
            ) : null}
          </>
        )}
      />
      <ConfirmAction
        open={single?.action === Pending.Suspend}
        onOpenChange={(open) => !open && setSingle(undefined)}
        title={`Suspend ${singleName}?`}
        consequence="They are signed out on every device and cannot sign in until restored."
        confirmLabel="Suspend account"
        onConfirm={async (reason) => {
          await authAdminClient.suspendUser({ userId: single?.user.user?.id ?? "", reason });
          await refresh();
        }}
      />
      <ConfirmAction
        open={single?.action === Pending.Restore}
        onOpenChange={(open) => !open && setSingle(undefined)}
        title={`Restore ${singleName}?`}
        consequence="They can sign in again."
        confirmLabel="Restore account"
        destructive={false}
        onConfirm={async (reason) => {
          await authAdminClient.restoreUser({ userId: single?.user.user?.id ?? "", reason });
          await refresh();
        }}
      />
      <ConfirmAction
        open={bulk === Pending.Suspend}
        onOpenChange={(open) => setBulk(open ? Pending.Suspend : Pending.None)}
        title={`Suspend ${active.length} account${active.length === 1 ? "" : "s"}?`}
        consequence="Each one is signed out everywhere and cannot sign in until restored. One audit entry per account."
        confirmLabel="Suspend accounts"
        quiet
        onConfirm={async (reason) => {
          try {
            await bulkApply(active, "Suspended", (row) => authAdminClient.suspendUser({ userId: row.user?.id ?? "", reason }));
            setSelected(new Set());
          } finally {
            await refresh();
          }
        }}
      />
      <ConfirmAction
        open={bulk === Pending.Restore}
        onOpenChange={(open) => setBulk(open ? Pending.Restore : Pending.None)}
        title={`Restore ${suspended.length} account${suspended.length === 1 ? "" : "s"}?`}
        consequence="They can sign in again. One audit entry per account."
        confirmLabel="Restore accounts"
        destructive={false}
        quiet
        onConfirm={async (reason) => {
          try {
            await bulkApply(suspended, "Restored", (row) => authAdminClient.restoreUser({ userId: row.user?.id ?? "", reason }));
            setSelected(new Set());
          } finally {
            await refresh();
          }
        }}
      />
    </AdminListPage>
  );
}
