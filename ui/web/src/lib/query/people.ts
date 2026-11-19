"use client";

import { Code } from "@connectrpc/connect";
import { useInfiniteQuery, useQuery, type QueryClient } from "@tanstack/react-query";

import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { apiClient } from "@/lib/connect/clients";
import { isCode } from "@/lib/errors";

export const peopleKeys = {
  search: (query: string) => ["people", "search-pages", query] as const,
  profiles: (userIds: string[]) => ["people", "profiles", ...userIds] as const,
  byUsername: (username: string) => ["people", "username", username] as const,
};

export function invalidateProfilesOf(queryClient: QueryClient, userID: string): Promise<void> {
  return queryClient.invalidateQueries({
    predicate: (query) =>
      query.queryKey[0] === "people" &&
      query.queryKey[1] === "profiles" &&
      query.queryKey.includes(userID),
  });
}

export function useSearchUsers(query: string, enabled = true) {
  const trimmed = query.trim();

  const result = useInfiniteQuery({
    queryKey: peopleKeys.search(trimmed),
    queryFn: ({ pageParam, signal }) => apiClient.searchUsers({ query: trimmed, pageSize: 20, pageToken: pageParam }, { signal }),
    initialPageParam: "",
    getNextPageParam: (lastPage) => lastPage.nextPageToken || undefined,
    staleTime: 30_000,
    enabled,
  });

  const people = result.data?.pages.flatMap((page) => page.users);
  return { ...result, data: people ? [...new Map(people.map((person) => [person.id, person])).values()] : undefined };
}

export function usePublicProfiles(userIDs: string[]) {
  const ids = [...new Set(userIDs)].sort();

  return useQuery({
    queryKey: peopleKeys.profiles(ids),
    queryFn: async (): Promise<Map<string, PublicProfile>> => {
      const resp = await apiClient.getUsersPublicProfiles({ userIds: ids });
      return new Map(resp.users.map((profile) => [profile.id, profile]));
    },
    enabled: ids.length > 0,
  });
}

export function usePublicProfileByUsername(username: string) {
  return useQuery({
    queryKey: peopleKeys.byUsername(username),
    queryFn: async (): Promise<PublicProfile | null> => {
      const resp = await apiClient.getPublicProfileByUsername({ username });
      return resp.user ?? null;
    },
    enabled: username !== "",
    retry: (failures, err) => !isCode(err, Code.NotFound) && failures < 2,
  });
}
