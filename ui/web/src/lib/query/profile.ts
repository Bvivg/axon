"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { OauthProvider, Session } from "@/gen/axon/auth/v1/auth_pb";
import { apiClient } from "@/lib/connect/clients";

export const profileKeys = {
  sessions: ["profile", "sessions"] as const,
  linkedProviders: ["profile", "linked-providers"] as const,
};

export interface ProfilePatch {
  email?: string;
  username?: string;
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

const sessionsRefetchInterval = 10_000;

export function useSessions() {
  return useQuery({
    queryKey: profileKeys.sessions,
    queryFn: async (): Promise<Session[]> => {
      const resp = await apiClient.listSessions({});
      return resp.sessions;
    },
    refetchInterval: sessionsRefetchInterval,
  });
}

export function useLinkedProviders() {
  return useQuery({
    queryKey: profileKeys.linkedProviders,
    queryFn: async (): Promise<OauthProvider[]> => {
      const resp = await apiClient.getMe({});
      return resp.linkedProviders;
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
