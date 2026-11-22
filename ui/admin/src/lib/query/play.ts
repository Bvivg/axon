"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { AnalysisReportEntry } from "@/gen/axon/analysis/v1/admin_pb";
import type { AdminUpload } from "@/gen/axon/chat/v1/admin_pb";
import type { RatingEntry } from "@/gen/axon/rating/v1/admin_pb";
import { analysisAdminClient, chatAdminClient, gameAdminClient, ratingAdminClient, tournamentAdminClient, tournamentClient } from "@/lib/connect/clients";
import { adminKeys, adminPageSize, useCursorList } from "@/lib/query/admin";

export const playKeys = {
  tournament: (id: string) => [...adminKeys.tournaments, "detail", id] as const,
  game: (id: string) => [...adminKeys.games, "detail", id] as const,
  queue: [...adminKeys.games, "queue"] as const,
  standings: [...adminKeys.ratings, "standings"] as const,
  uploads: [...adminKeys.rooms, "uploads"] as const,
  analysis: ["admin", "analysis"] as const,
};

export function useAdminTournament(id: string) {
  return useQuery({
    queryKey: playKeys.tournament(id),
    queryFn: async ({ signal }) => {
      const tournament = (await tournamentClient.getTournament({ tournamentId: id }, { signal })).tournament;
      if (!tournament) throw new Error("the server returned no tournament");
      return tournament;
    },
    enabled: !!id,
    refetchOnWindowFocus: false,
  });
}

function useTournamentMutation<V extends { tournamentId: string }, R>(run: (vars: V) => Promise<R>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: run,
    onSettled: (_data, _error, vars) => Promise.all([playKeys.tournament(vars.tournamentId), adminKeys.tournaments, adminKeys.audit].map((queryKey) => client.invalidateQueries({ queryKey }))),
  });
}

export function useUpdateTournament() {
  return useTournamentMutation(async (vars: { tournamentId: string; name: string; capacity: number; rounds: number; paths: string[]; etag: string; reason: string }) => {
    const { paths, ...rest } = vars;
    const tournament = (await tournamentAdminClient.updateTournament({ ...rest, updateMask: { paths } })).tournament;
    if (!tournament) throw new Error("the server returned no tournament");
    return tournament;
  });
}

export function useRemoveTournamentPlayer() {
  return useTournamentMutation((vars: { tournamentId: string; userId: string; reason: string }) => tournamentAdminClient.removeTournamentPlayer(vars));
}

export function useTournamentControls() {
  const client = useQueryClient();
  const settle = (id: string) => Promise.all([playKeys.tournament(id), adminKeys.tournaments, adminKeys.games, adminKeys.audit].map((queryKey) => client.invalidateQueries({ queryKey })));
  const start = useMutation({
    mutationFn: (vars: { tournamentId: string; reason: string }) => tournamentClient.startTournament(vars),
    onSettled: (_d, _e, vars) => settle(vars.tournamentId),
  });
  const cancel = useMutation({
    mutationFn: (vars: { tournamentId: string; reason: string }) => tournamentClient.cancelTournament(vars),
    onSettled: (_d, _e, vars) => settle(vars.tournamentId),
  });
  return { start, cancel };
}

export function useAdminGame(id: string) {
  return useQuery({
    queryKey: playKeys.game(id),
    queryFn: async ({ signal }) => gameAdminClient.getGameSession({ sessionId: id }, { signal }),
    enabled: !!id,
  });
}

export function useMatchQueue(game: string) {
  return useQuery({
    queryKey: [...playKeys.queue, game],
    queryFn: async ({ signal }) => (await gameAdminClient.listMatchQueue({ game }, { signal })).entries,
    refetchInterval: 10_000,
  });
}

export function useRemoveFromQueue() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (vars: { userId: string; game: string; reason: string }) => gameAdminClient.removeFromMatchQueue(vars),
    onSettled: () => Promise.all([playKeys.queue, adminKeys.audit].map((queryKey) => client.invalidateQueries({ queryKey }))),
  });
}

export function useStandings(filters: { game: string; user: string }, enabled = true) {
  return useCursorList<RatingEntry>([...playKeys.standings, filters], async (cursor, signal) => {
    const resp = await ratingAdminClient.listRatings({ game: filters.game, userId: filters.user.trim(), cursor, pageSize: adminPageSize }, { signal });
    return { items: resp.ratings, next: resp.nextCursor };
  }, enabled);
}

export function useChatUploads(filters: { uploader: string; room: string; kind: string }, enabled = true) {
  return useCursorList<AdminUpload>([...playKeys.uploads, filters], async (cursor, signal) => {
    const resp = await chatAdminClient.listChatUploads({ uploaderId: filters.uploader.trim(), roomId: filters.room.trim(), kind: filters.kind, cursor, pageSize: adminPageSize }, { signal });
    return { items: resp.uploads, next: resp.nextCursor };
  }, enabled);
}

export function useAnalysisReports(filters: { status: string }, enabled = true) {
  return useCursorList<AnalysisReportEntry>([...playKeys.analysis, filters], async (cursor, signal) => {
    const resp = await analysisAdminClient.listAnalysisReports({ status: filters.status, cursor, pageSize: adminPageSize }, { signal });
    return { items: resp.reports, next: resp.nextCursor };
  }, enabled);
}
