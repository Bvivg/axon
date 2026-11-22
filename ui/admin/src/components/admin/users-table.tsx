"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Ban, RotateCcw, Users } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { AdminListPage, count, userHref, When } from "@/components/admin/admin-ui";
import { BulkBar, bulkApply } from "@/components/admin/bulk-bar";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { DataTable, SortDirection, type DataTableSort } from "@/components/admin/data-table";
import { ChipFilter, SearchFilter, SelectFilter, Toolbar } from "@/components/admin/filters";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { UserStatus, type AdminUser } from "@/gen/axon/auth/v1/admin_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { bulkLimit } from "@/lib/admin/bulk";
import { useUrlState } from "@/lib/admin/url-state";
import { useSession } from "@/lib/auth/session";
import { initials, personName } from "@/lib/format";
import { authAdminClient } from "@/lib/connect/clients";
import { adminKeys, flatten, useAdminUsers, useRefreshAfterBulk, useRoles, UserSortOrder, UserStatusFilter } from "@/lib/query/admin";

enum BulkAction {
  None = "",
  Suspend = "suspend",
  Restore = "restore",
}

const keys = ["query", "status", "role", "sort"] as const;

const statusOptions = [
  { value: UserStatusFilter.All, label: "All" },
  { value: UserStatusFilter.Active, label: "Active" },
  { value: UserStatusFilter.Suspended, label: "Suspended" },
];

export function StatusBadge({ user }: { user: AdminUser }) {
  return user.status === UserStatus.SUSPENDED ? <Badge variant="destructive" dot>Suspended</Badge> : <span className="text-muted-foreground">Active</span>;
}

const columns: ColumnDef<AdminUser, unknown>[] = [
  {
    id: "account",
    header: "Account",
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
  { id: "email", header: "Email", cell: ({ row }) => <span className="break-all">{row.original.user?.email}</span> },
  {
    id: "roles",
    header: "Roles",
    cell: ({ row }) => (
      <span className="flex flex-wrap gap-1">
        {row.original.user?.roles.map((role) => <Badge key={role} variant={role === "admin" ? "signal" : "outline"}>{role}</Badge>)}
      </span>
    ),
  },
  { id: "status", header: "Status", cell: ({ row }) => <StatusBadge user={row.original} /> },
  { id: "sessions", header: "Sessions", cell: ({ row }) => <span className="tabular-nums">{row.original.activeSessions}</span> },
  { id: "joined", header: "Joined", cell: ({ row }) => <When at={row.original.user?.createdAt} /> },
  { id: "seen", header: "Last seen", cell: ({ row }) => <When at={row.original.lastSeenAt} /> },
];

export function UsersTable() {
  const access = useAdminAccess();
  const [state, update, clear] = useUrlState(keys);
  const users = useAdminUsers(state);
  const roles = useRoles(access.can(Permission.RolesRead));
  const rows = useMemo(() => flatten(users.data), [users.data]);
  const filtered = !!(state.query || state.status || state.role);
  const { user: me } = useSession();
  const refresh = useRefreshAfterBulk([adminKeys.users]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulk, setBulk] = useState(BulkAction.None);
  const mutate = access.can(Permission.UsersMutate);
  const picked = rows.filter((row) => row.user && selected.has(row.user.id));
  const active = picked.filter((row) => row.status !== UserStatus.SUSPENDED);
  const suspended = picked.filter((row) => row.status === UserStatus.SUSPENDED);

  const roleOptions = useMemo(() => [
    { value: "", label: "Any role" },
    ...(roles.data ?? []).map((role) => ({ value: role.name, label: role.name })),
  ], [roles.data]);

  const sort: DataTableSort = {
    column: state.sort === UserSortOrder.Email ? "email" : "joined",
    direction: state.sort === UserSortOrder.Oldest || state.sort === UserSortOrder.Email ? SortDirection.Ascending : SortDirection.Descending,
    sortable: ["joined", "email"],
    onSort: (column) => update({ sort: column === "email" ? UserSortOrder.Email : state.sort === UserSortOrder.Newest ? UserSortOrder.Oldest : UserSortOrder.Newest }),
  };

  return (
    <AdminListPage
      title="Users"
      subtitle={count(users.data?.pages[0]?.total, "account")}
      toolbar={
        <>
          <Toolbar>
            <SearchFilter label="Search by name, username or email" value={state.query} onChange={(query) => update({ query })} />
            <ChipFilter label="Status" value={state.status} options={statusOptions} onChange={(status) => update({ status })} />
            {roles.data ? <SelectFilter label="Role" value={state.role} options={roleOptions} onChange={(role) => update({ role })} /> : null}
          </Toolbar>
          <BulkBar count={picked.length} limit={bulkLimit} onClear={() => setSelected(new Set())}>
            <Button variant="destructive-outline" size="sm" disabled={active.length === 0} onClick={() => setBulk(BulkAction.Suspend)}><Ban aria-hidden /> Suspend {active.length}</Button>
            <Button variant="outline" size="sm" disabled={suspended.length === 0} onClick={() => setBulk(BulkAction.Restore)}><RotateCcw aria-hidden /> Restore {suspended.length}</Button>
          </BulkBar>
        </>
      }
    >
      <DataTable
        label="Users"
        columns={columns}
        rows={rows}
        rowKey={(row) => row.user?.id ?? ""}
        rowHref={(row) => userHref(row.user?.id ?? "")}
        status={users.status}
        error={users.error}
        onRetry={() => void users.refetch()}
        filtered={filtered}
        onClearFilters={clear}
        empty={{ icon: Users, title: "No accounts yet" }}
        hasMore={users.hasNextPage}
        loadingMore={users.isFetchingNextPage}
        onLoadMore={() => void users.fetchNextPage()}
        sort={sort}
        selection={mutate ? { selected, onChange: setSelected, selectable: (row) => !!row.user && row.user.id !== me?.id, limit: bulkLimit } : undefined}
      />
      <ConfirmAction
        open={bulk === BulkAction.Suspend}
        onOpenChange={(open) => setBulk(open ? BulkAction.Suspend : BulkAction.None)}
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
        open={bulk === BulkAction.Restore}
        onOpenChange={(open) => setBulk(open ? BulkAction.Restore : BulkAction.None)}
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
