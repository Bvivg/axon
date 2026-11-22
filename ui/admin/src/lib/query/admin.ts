"use client";

import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code } from "@connectrpc/connect";
import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData } from "@tanstack/react-query";

import { AuditOutcome, UserStatus, type AuditEvent, type AdminUser } from "@/gen/axon/auth/v1/admin_pb";
import type { Role } from "@/gen/axon/auth/v1/auth_pb";
import type { Call } from "@/gen/axon/calls/v1/calls_pb";
import type { AdminRoom, AdminUpload } from "@/gen/axon/chat/v1/admin_pb";
import type { Message } from "@/gen/axon/chat/v1/chat_pb";
import { GameSessionFilter } from "@/gen/axon/game/v1/admin_pb";
import type { Session } from "@/gen/axon/game/v1/game_pb";
import { apiClient, authAdminClient, callsAdminClient, chatAdminClient, gameAdminClient, tournamentClient } from "@/lib/connect/clients";
import { isCode } from "@/lib/errors";
import type { ListPage, ListRequest } from "@/lib/admin/list";
import { fetchProfiles, peopleKeys } from "@/lib/query/people";

export const adminPageSize = 50;

export const adminKeys = {
  all: ["admin"] as const,
  roles: ["admin", "roles"] as const,
  users: ["admin", "users"] as const,
  user: (id: string) => ["admin", "users", "detail", id] as const,
  audit: ["admin", "audit"] as const,
  games: ["admin", "games"] as const,
  tournaments: ["admin", "tournaments"] as const,
  ratings: ["admin", "ratings"] as const,
  rooms: ["admin", "rooms"] as const,
  room: (id: string) => ["admin", "rooms", "detail", id] as const,
  messages: (id: string) => ["admin", "rooms", "messages", id] as const,
  calls: ["admin", "calls"] as const,
  overview: ["admin", "overview"] as const,
};

export interface Page<T> {
  items: T[];
  next: string;
  total?: bigint;
}

export function flatten<T>(data?: InfiniteData<Page<T>>): T[] {
  return data?.pages.flatMap((page) => page.items) ?? [];
}

export function useAdminProfiles(userIDs: string[]) {
  const ids = [...new Set(userIDs.filter(Boolean))].sort();
  return useQuery({
    queryKey: peopleKeys.profiles(ids),
    queryFn: () => fetchProfiles((request) => apiClient.getUsersPublicProfiles(request), ids),
    enabled: ids.length > 0,
    placeholderData: keepPreviousData,
  });
}

export function featureOff(error: unknown): boolean {
  return isCode(error, Code.Unimplemented);
}

function retryUnlessOff(failures: number, error: unknown): boolean {
  return !featureOff(error) && failures < 3;
}

export function useCursorList<T>(key: readonly unknown[], load: (cursor: string, signal: AbortSignal) => Promise<Page<T>>, enabled = true) {
  return useInfiniteQuery({
    queryKey: key,
    queryFn: ({ pageParam, signal }) => load(pageParam, signal),
    initialPageParam: "",
    getNextPageParam: (last: Page<T>) => last.next || undefined,
    enabled,
    retry: retryUnlessOff,
  });
}

export async function loadUsers(request: ListRequest, query: string, signal: AbortSignal): Promise<ListPage<AdminUser>> {
  const resp = await authAdminClient.listUsers({ query, filter: request.filter, orderBy: request.orderBy, offset: request.offset, pageSize: request.pageSize }, { signal });
  return { items: resp.users, total: Number(resp.total) };
}

export function useAdminUser(id: string) {
  return useQuery({
    queryKey: adminKeys.user(id),
    queryFn: async ({ signal }) => authAdminClient.getUser({ userId: id }, { signal }),
    enabled: !!id,
    refetchOnWindowFocus: false,
  });
}

type QueryKeys = readonly (readonly unknown[])[];

function useInvalidating<V>(keys: QueryKeys | ((vars: V) => QueryKeys), run: (vars: V) => Promise<unknown>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSettled: (_data, _error, vars) => Promise.all([...(typeof keys === "function" ? keys(vars) : keys), adminKeys.audit, adminKeys.overview].map((queryKey) => client.invalidateQueries({ queryKey }))),
  });
}

export function useSuspendUser() {
  return useInvalidating([adminKeys.users], (vars: { userId: string; reason: string }) => authAdminClient.suspendUser(vars));
}

export function useRestoreUser() {
  return useInvalidating([adminKeys.users], (vars: { userId: string; reason: string }) => authAdminClient.restoreUser(vars));
}

export function useRevokeUserSessions() {
  return useInvalidating([adminKeys.users], (vars: { userId: string; reason: string }) => authAdminClient.revokeUserSessions(vars));
}

export function useRoles(enabled = true) {
  return useQuery({
    queryKey: adminKeys.roles,
    queryFn: async (): Promise<Role[]> => (await apiClient.listRoles({})).roles,
    enabled,
    staleTime: 60_000,
  });
}

export interface AuditFilters {
  service: string;
  action: string;
  outcome: string;
  actor: string;
  targetType: string;
  target: string;
  since: string;
  until: string;
}

const outcomes: Record<string, AuditOutcome> = {
  succeeded: AuditOutcome.SUCCEEDED,
  denied: AuditOutcome.DENIED,
  failed: AuditOutcome.FAILED,
};

function dayBoundary(raw: string, end: boolean) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(raw)) return undefined;
  const at = new Date(`${raw}T00:00:00`);
  if (Number.isNaN(at.getTime())) return undefined;
  if (end) at.setDate(at.getDate() + 1);
  return timestampFromDate(at);
}

function auditRequest(filters: AuditFilters) {
  return {
    service: filters.service,
    action: filters.action.trim(),
    outcome: outcomes[filters.outcome] ?? AuditOutcome.UNSPECIFIED,
    actorId: filters.actor.trim(),
    targetType: filters.targetType,
    targetId: filters.target.trim(),
    since: dayBoundary(filters.since, false),
    until: dayBoundary(filters.until, true),
  };
}

export function useAuditEvents(filters: AuditFilters, enabled = true, pageSize = adminPageSize) {
  return useCursorList<AuditEvent>([...adminKeys.audit, filters, pageSize], async (cursor, signal) => {
    const resp = await authAdminClient.listAuditEvents({ ...auditRequest(filters), cursor, pageSize }, { signal });
    return { items: resp.events, next: resp.nextCursor };
  }, enabled);
}

export async function loadAudit(request: ListRequest, signal: AbortSignal): Promise<ListPage<AuditEvent>> {
  const resp = await authAdminClient.listAuditEvents({ filter: request.filter, orderBy: request.orderBy, offset: request.offset, pageSize: request.pageSize }, { signal });
  return { items: resp.events, total: Number(resp.total ?? 0) };
}

export function useExportAudit() {
  return useMutation({
    mutationFn: (query: { filter: string; orderBy: string }) => authAdminClient.exportAuditEvents({ filter: { filter: query.filter, orderBy: query.orderBy } }),
  });
}

export async function loadGames(request: ListRequest, signal: AbortSignal): Promise<ListPage<Session>> {
  const resp = await gameAdminClient.listGameSessions({ filter: request.filter, orderBy: request.orderBy, offset: request.offset, pageSize: request.pageSize }, { signal });
  return { items: resp.sessions, total: Number(resp.total) };
}

export function useCancelGame() {
  return useInvalidating([adminKeys.games, adminKeys.tournaments], (vars: { sessionId: string; reason: string }) => gameAdminClient.cancelGameSession(vars));
}

export function useStartTournament() {
  return useInvalidating([adminKeys.tournaments, adminKeys.games], (vars: { tournamentId: string; reason: string }) => tournamentClient.startTournament(vars));
}

export function useCancelTournament() {
  return useInvalidating([adminKeys.tournaments, adminKeys.games], (vars: { tournamentId: string; reason: string }) => tournamentClient.cancelTournament(vars));
}

export async function loadRooms(request: ListRequest, query: string, signal: AbortSignal): Promise<ListPage<AdminRoom>> {
  const resp = await chatAdminClient.listChatRooms({ query, filter: request.filter, orderBy: request.orderBy, offset: request.offset, pageSize: request.pageSize }, { signal });
  return { items: resp.rooms, total: Number(resp.total) };
}

export async function loadUploads(request: ListRequest, signal: AbortSignal): Promise<ListPage<AdminUpload>> {
  const resp = await chatAdminClient.listChatUploads({ filter: request.filter, orderBy: request.orderBy, offset: request.offset, pageSize: request.pageSize }, { signal });
  return { items: resp.uploads, total: Number(resp.total ?? 0) };
}

export function useAdminRoom(id: string) {
  return useQuery({
    queryKey: adminKeys.room(id),
    queryFn: async ({ signal }) => chatAdminClient.getChatRoom({ roomId: id }, { signal }),
    enabled: !!id,
  });
}

export function useAdminRoomMessages(roomId: string, reason: string) {
  return useInfiniteQuery({
    queryKey: [...adminKeys.messages(roomId), reason],
    queryFn: async ({ pageParam, signal }): Promise<{ items: Message[]; before?: bigint }> => {
      const resp = await chatAdminClient.listChatRoomMessages({ roomId, reason, beforeSeq: pageParam, pageSize: adminPageSize }, { signal });
      return { items: resp.messages, before: resp.nextBeforeSeq };
    },
    initialPageParam: undefined as bigint | undefined,
    getNextPageParam: (last) => last.before,
    enabled: !!roomId && reason.length >= 3,
    staleTime: Infinity,
    retry: false,
  });
}

export function useDeleteChatMessage() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (vars: { messageId: string; reason: string }) => chatAdminClient.deleteChatMessage(vars),
    onSuccess: (resp) => {
      const deleted = resp.message;
      if (!deleted) return;
      client.setQueriesData<InfiniteData<{ items: Message[]; before?: bigint }>>({ queryKey: adminKeys.messages(deleted.roomId) }, (data) => data && {
        ...data,
        pages: data.pages.map((page) => ({ ...page, items: page.items.map((item) => item.id === deleted.id ? deleted : item) })),
      });
    },
    onSettled: () => client.invalidateQueries({ queryKey: adminKeys.audit }),
  });
}

export function useRemoveChatMember() {
  return useInvalidating((vars: { roomId: string; userId: string; reason: string }) => [[...adminKeys.rooms, "list"], adminKeys.room(vars.roomId)], (vars) => chatAdminClient.removeChatMember(vars));
}

export async function loadCalls(request: ListRequest, signal: AbortSignal): Promise<ListPage<Call>> {
  const resp = await callsAdminClient.listAllCalls({ filter: request.filter, orderBy: request.orderBy, offset: request.offset, pageSize: request.pageSize }, { signal });
  return { items: resp.calls, total: Number(resp.total ?? 0) };
}

export function useTerminateCall() {
  return useInvalidating([adminKeys.calls], (vars: { callId: string; reason: string }) => callsAdminClient.terminateCall(vars));
}

export interface AdminCounts {
  users?: bigint;
  suspended?: bigint;
  liveGames?: bigint;
  rooms?: bigint;
  activeCalls?: bigint;
}

export function useAdminCount<K extends keyof AdminCounts>(key: K, enabled: boolean) {
  return useQuery({
    queryKey: [...adminKeys.overview, key],
    queryFn: async ({ signal }): Promise<bigint> => {
      switch (key) {
        case "users": return (await authAdminClient.listUsers({ pageSize: 1 }, { signal })).total;
        case "suspended": return (await authAdminClient.listUsers({ pageSize: 1, status: UserStatus.SUSPENDED }, { signal })).total;
        case "liveGames": return (await gameAdminClient.listGameSessions({ pageSize: 1, status: GameSessionFilter.LIVE }, { signal })).total;
        case "rooms": return (await chatAdminClient.listChatRooms({ pageSize: 1 }, { signal })).total;
        default: return (await callsAdminClient.listAllCalls({ pageSize: 1, activeOnly: true }, { signal })).activeTotal;
      }
    },
    enabled,
    retry: retryUnlessOff,
    refetchInterval: (query) => featureOff(query.state.error) ? false : 30_000,
  });
}

function useUserMutation<V extends { userId: string }, R>(run: (vars: V) => Promise<R>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSettled: (_data, _error, vars) => Promise.all([adminKeys.user(vars.userId), [...adminKeys.users, "list"], adminKeys.roles, adminKeys.audit].map((queryKey) => client.invalidateQueries({ queryKey }))),
  });
}

export function useUpdateUserProfile() {
  return useUserMutation((vars: { userId: string; displayName: string; etag: string; reason: string }) => authAdminClient.updateUserProfile(vars));
}

export function useSetUserRoles() {
  return useUserMutation((vars: { userId: string; roles: string[]; reason: string }) => authAdminClient.setUserRoles(vars));
}

export function useUnlinkAccount() {
  return useUserMutation((vars: { userId: string; provider: string; reason: string }) => authAdminClient.unlinkOAuthAccount(vars));
}

export function useRefreshAfterBulk(keys: QueryKeys) {
  const client = useQueryClient();
  return () => Promise.all([...keys, adminKeys.audit, adminKeys.overview].map((queryKey) => client.invalidateQueries({ queryKey })));
}
