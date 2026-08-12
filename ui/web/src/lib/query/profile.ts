"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { Session } from "@/gen/axon/auth/v1/auth_pb";
import { apiClient } from "@/lib/connect/clients";

export const profileKeys = {
  sessions: ["profile", "sessions"] as const,
};

export interface ProfilePatch {
  email?: string;
  firstName?: string;
  lastName?: string;
  nickname?: string;
}

export function useUpdateProfile() {
  return useMutation({
    mutationFn: async (patch: ProfilePatch) => {
      const resp = await apiClient.updateProfile(patch);
      if (!resp.user) {
        throw new Error("the server returned no user");
      }
      return resp.user;
    },
  });
}

export function useSessions() {
  return useQuery({
    queryKey: profileKeys.sessions,
    queryFn: async (): Promise<Session[]> => {
      const resp = await apiClient.listSessions({});
      return resp.sessions;
    },
  });
}

export function useRevokeSession() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (sessionId: string) => {
      await apiClient.revokeSession({ sessionId });
      return sessionId;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: profileKeys.sessions });
    },
  });
}
