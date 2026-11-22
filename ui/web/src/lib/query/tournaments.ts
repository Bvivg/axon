import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { TournamentFormat, TournamentStatus, type Tournament } from "@/gen/axon/tournament/v1/tournament_pb";
import { gameClient, tournamentClient } from "@/lib/connect/clients";
import { sessionKey } from "@/lib/query/games";

export const tournamentsKey = ["tournament", "list"] as const;
export const tournamentKey = (id: string) => ["tournament", "one", id] as const;

export const tournamentPollMs = 2_000;
export const tournamentListPollMs = 10_000;

export const tournamentsHref = "/games/tournaments";
export const tournamentHref = (id: string) => `${tournamentsHref}/${id}`;

export function tournamentOver(t: Tournament | undefined) {
  return t?.status === TournamentStatus.FINISHED || t?.status === TournamentStatus.CANCELLED;
}

export function tournamentStatusLabel(status: TournamentStatus) {
  switch (status) {
    case TournamentStatus.REGISTRATION: return "Registration";
    case TournamentStatus.SEEDING: return "Seeding";
    case TournamentStatus.RUNNING: return "In progress";
    case TournamentStatus.FINISHED: return "Finished";
    case TournamentStatus.CANCELLED: return "Cancelled";
    default: return "Unavailable";
  }
}

export function tournamentFormatLabel(format: TournamentFormat) {
  return format === TournamentFormat.SWISS ? "Swiss" : "Single elimination";
}

export function useTournaments() {
  return useQuery({
    queryKey: tournamentsKey,
    queryFn: async ({ signal }) => (await tournamentClient.listTournaments({ limit: 50 }, { signal })).tournaments,
    refetchInterval: tournamentListPollMs,
    refetchOnMount: "always",
  });
}

export function useTournament(id: string) {
  return useQuery({
    queryKey: tournamentKey(id),
    queryFn: async ({ signal }) => {
      const { tournament } = await tournamentClient.getTournament({ tournamentId: id }, { signal });
      if (!tournament) throw new Error("Tournament not found");
      return tournament;
    },
    refetchInterval: query => tournamentOver(query.state.data) ? false : tournamentPollMs,
  });
}

export const tournamentGameKey = (id: string) => ["tournament", "game", id] as const;

export function useTournamentGame(id: string | undefined, enabled: boolean) {
  const client = useQueryClient();
  return useQuery({
    queryKey: tournamentGameKey(id ?? ""),
    queryFn: async ({ signal }) => {
      try {
        const { session } = await gameClient.getSession({ sessionId: id! }, { signal });
        if (session) client.setQueryData(sessionKey(session.id), session);
        return session ?? null;
      } catch (error) {
        if (ConnectError.from(error).code === Code.NotFound) return null;
        throw error;
      }
    },
    enabled: enabled && !!id,
    refetchInterval: query => query.state.data ? false : tournamentPollMs,
  });
}

export enum TournamentAction {
  Register = "register",
  Withdraw = "withdraw",
  Start = "start",
  Cancel = "cancel",
}

export function useTournamentAction(id: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async ({ action, reason }: { action: TournamentAction; reason?: string }) => {
      const request = { tournamentId: id };
      const response = action === TournamentAction.Register ? await tournamentClient.registerForTournament(request)
        : action === TournamentAction.Withdraw ? await tournamentClient.withdrawFromTournament(request)
        : action === TournamentAction.Start ? await tournamentClient.startTournament({ ...request, reason })
        : await tournamentClient.cancelTournament({ ...request, reason });
      if (!response.tournament) throw new Error("Tournament not found");
      return response.tournament;
    },
    onSuccess: tournament => {
      client.setQueryData(tournamentKey(tournament.id), tournament);
      void client.invalidateQueries({ queryKey: tournamentsKey });
    },
  });
}

export type NewTournament = { name: string; game: string; format: TournamentFormat; capacity: number; rounds?: number };

export function useCreateTournament() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async ({ requestId, ...spec }: NewTournament & { requestId: string }) => {
      const { tournament } = await tournamentClient.createTournament({ requestId, ...spec });
      if (!tournament) throw new Error("Could not create the tournament.");
      return tournament;
    },
    onSuccess: tournament => {
      client.setQueryData(tournamentKey(tournament.id), tournament);
      void client.invalidateQueries({ queryKey: tournamentsKey });
    },
  });
}
