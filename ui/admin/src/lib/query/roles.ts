"use client";

import type { MessageInitShape } from "@bufbuild/protobuf";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { AdminRole, AdminRoleSchema, PermissionInfo } from "@/gen/axon/auth/v1/admin_pb";
import { authAdminClient } from "@/lib/connect/clients";
import { adminKeys } from "@/lib/query/admin";

export const roleKeys = {
  all: adminKeys.roles,
  list: [...adminKeys.roles, "admin-list"] as const,
  detail: (id: string) => [...adminKeys.roles, "detail", id] as const,
  permissions: [...adminKeys.roles, "permissions"] as const,
};

export function useAdminRoles(enabled = true) {
  return useQuery({
    queryKey: roleKeys.list,
    queryFn: async ({ signal }): Promise<AdminRole[]> => (await authAdminClient.listAdminRoles({}, { signal })).roles,
    enabled,
  });
}

export function useAdminRole(id: string) {
  return useQuery({
    queryKey: roleKeys.detail(id),
    queryFn: async ({ signal }) => {
      const role = (await authAdminClient.getRole({ roleId: id }, { signal })).role;
      if (!role) throw new Error("the server returned no role");
      return role;
    },
    enabled: !!id,
    refetchOnWindowFocus: false,
  });
}

export function usePermissionCatalog(enabled = true) {
  return useQuery({
    queryKey: roleKeys.permissions,
    queryFn: async ({ signal }): Promise<PermissionInfo[]> => (await authAdminClient.listPermissions({}, { signal })).permissions,
    enabled,
    staleTime: 5 * 60_000,
  });
}

function useRoleMutation<V, R>(run: (vars: V) => Promise<R>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSettled: () => Promise.all([roleKeys.all, adminKeys.users, adminKeys.audit].map((queryKey) => client.invalidateQueries({ queryKey }))),
  });
}

export type RoleInit = MessageInitShape<typeof AdminRoleSchema>;

export function useCreateRole() {
  return useRoleMutation(async (vars: { role: RoleInit; reason: string; requestId: string }) => {
    const role = (await authAdminClient.createRole(vars)).role;
    if (!role) throw new Error("the server returned no role");
    return role;
  });
}

export function useUpdateRole() {
  return useRoleMutation(async (vars: { role: RoleInit; paths: string[]; reason: string }) => {
    const role = (await authAdminClient.updateRole({ role: vars.role, updateMask: { paths: vars.paths }, reason: vars.reason })).role;
    if (!role) throw new Error("the server returned no role");
    return role;
  });
}

export function useDeleteRole() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (vars: { roleId: string; etag: string; reason: string }) => authAdminClient.deleteRole(vars),
    onSettled: () => Promise.all([roleKeys.list, adminKeys.users, adminKeys.audit].map((queryKey) => client.invalidateQueries({ queryKey }))),
  });
}

export function groupPermissions(catalog: PermissionInfo[]): [string, PermissionInfo[]][] {
  const groups = new Map<string, PermissionInfo[]>();
  for (const permission of [...catalog].sort((a, b) => a.name.localeCompare(b.name))) {
    const list = groups.get(permission.namespace) ?? [];
    list.push(permission);
    groups.set(permission.namespace, list);
  }
  return [...groups.entries()];
}
