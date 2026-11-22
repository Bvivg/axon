"use client";

import { useQuery } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";

import { useSession } from "@/lib/auth/session";
import { StaffRole, type Permission } from "@/lib/admin/permissions";
import { authAdminClient } from "@/lib/connect/clients";

export { Permission, StaffRole } from "@/lib/admin/permissions";

export const permissionsKey = ["admin", "permissions"] as const;

export enum AccessStatus {
  Pending = "pending",
  Error = "error",
  Ready = "ready",
}

export interface AdminAccess {
  status: AccessStatus;
  error: unknown;
  staff: boolean;
  admin: boolean;
  can: (permission: Permission) => boolean;
  refetch: () => void;
}

const staffRoles = new Set<string>(Object.values(StaffRole));

export function useAdminAccess(): AdminAccess {
  const { status, user } = useSession();
  const roles = user?.roles;
  const staff = status === "authenticated" && !!roles?.some((role) => staffRoles.has(role));
  const admin = status === "authenticated" && !!roles?.includes(StaffRole.Admin);
  const query = useQuery({
    queryKey: [...permissionsKey, user?.id ?? ""],
    queryFn: async () => new Set((await authAdminClient.getMyPermissions({})).permissions),
    enabled: staff,
    staleTime: 30_000,
  });
  const granted = query.data;
  const { refetch, isError, error } = query;
  const can = useCallback((permission: Permission) => !!granted?.has(permission), [granted]);
  return useMemo(() => ({
    status: !staff ? (status === "restoring" ? AccessStatus.Pending : AccessStatus.Ready) : isError ? AccessStatus.Error : granted ? AccessStatus.Ready : AccessStatus.Pending,
    error,
    staff: staff && !!granted && [...granted].some((permission) => permission.startsWith("admin.")),
    admin,
    can,
    refetch: () => void refetch(),
  }), [staff, status, isError, granted, error, admin, can, refetch]);
}
