"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { AnalysisReportEntry } from "@/gen/axon/analysis/v1/admin_pb";
import type { QueueEntry } from "@/gen/axon/game/v1/admin_pb";
import type { RatingChangeEntry, RatingEntry } from "@/gen/axon/rating/v1/admin_pb";
import type { Tournament } from "@/gen/axon/tournament/v1/tournament_pb";
import type { ListPage, ListRequest } from "@/lib/admin/list";
import { analysisAdminClient, gameAdminClient, ratingAdminClient, tournamentAdminClient, tournamentClient } from "@/lib/connect/clients";
import { adminKeys } from "@/lib/query/admin";

const page = (request: ListRequest) => ({ filter: request.filter, orderBy: request.orderBy, offset: request.offset, pageSize: request.pageSize });

export const playKeys = {
  tournament: (id: string) => [...adminKeys.tournaments, "detail", id] as const,
  game: (id: string) => [...adminKeys.games, "detail", id] as const,
  queue: [...adminKeys.games, "queue"] as const,
  standings: [...adminKeys.ratings, "standings"] as const,
  changes: [...adminKeys.ratings, "changes"] as const,
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

export async function loadQueue(request: ListRequest, signal: AbortSignal): Promise<ListPage<QueueEntry>> {
  const resp = await gameAdminClient.listMatchQueue({ game: "", ...page(request) }, { signal });
  return { items: resp.entries, total: Number(resp.total ?? resp.entries.length) };
}

export function useRemoveFromQueue() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (vars: { userId: string; game: string; reason: string }) => gameAdminClient.removeFromMatchQueue(vars),
    onSettled: () => Promise.all([playKeys.queue, adminKeys.audit].map((queryKey) => client.invalidateQueries({ queryKey }))),
  });
}

export async function loadStandings(request: ListRequest, signal: AbortSignal): Promise<ListPage<RatingEntry>> {
  const resp = await ratingAdminClient.listRatings({ game: "", userId: "", cursor: "", ...page(request) }, { signal });
  return { items: resp.ratings, total: Number(resp.total ?? 0) };
}

export async function loadRatingChanges(request: ListRequest, signal: AbortSignal): Promise<ListPage<RatingChangeEntry>> {
  const resp = await ratingAdminClient.listRatingChanges({ game: "", userId: "", cursor: "", ...page(request) }, { signal });
  return { items: resp.changes, total: Number(resp.total ?? 0) };
}

export async function loadAnalysis(request: ListRequest, signal: AbortSignal): Promise<ListPage<AnalysisReportEntry>> {
  const resp = await analysisAdminClient.listAnalysisReports({ status: "", cursor: "", ...page(request) }, { signal });
  return { items: resp.reports, total: Number(resp.total ?? 0) };
}

export async function loadTournaments(request: ListRequest, query: string, signal: AbortSignal): Promise<ListPage<Tournament>> {
  const resp = await tournamentAdminClient.listAdminTournaments({ query, ...page(request) }, { signal });
  return { items: resp.tournaments, total: Number(resp.total) };
}
