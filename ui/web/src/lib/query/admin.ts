"use client";

import { useMutation, useQuery } from "@tanstack/react-query";

import type { Role, User } from "@/gen/axon/auth/v1/auth_pb";
import { apiClient } from "@/lib/connect/clients";

export const adminKeys = {
  roles: ["admin", "roles"] as const,
};

export function useRoles() {
  return useQuery({
    queryKey: adminKeys.roles,
    queryFn: async (): Promise<Role[]> => {
      const resp = await apiClient.listRoles({});
      return resp.roles;
    },
  });
}

export function useLookupUser() {
  return useMutation({
    mutationFn: async (email: string): Promise<User> => {
      const resp = await apiClient.getUserByEmail({ email });
      if (!resp.user) {
        throw new Error("the server returned no user");
      }
      return resp.user;
    },
  });
}

export function useAssignRole() {
  return useMutation({
    mutationFn: async (vars: { userId: string; roleId: string }) => {
      await apiClient.assignRole(vars);
    },
  });
}

export function useRevokeRole() {
  return useMutation({
    mutationFn: async (vars: { userId: string; roleId: string }) => {
      await apiClient.revokeRole(vars);
    },
  });
}
