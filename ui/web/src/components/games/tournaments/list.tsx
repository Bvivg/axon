"use client";

import { Trophy } from "lucide-react";

import { GameIcon } from "@/components/games/catalogue";
import { CreateTournamentDialog } from "@/components/games/tournaments/create-dialog";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { ListItem } from "@/components/ui/list-item";
import { Pane } from "@/components/ui/pane";
import { Skeleton } from "@/components/ui/skeleton";
import { TournamentStatus, type Tournament } from "@/gen/axon/tournament/v1/tournament_pb";
import { gameKey, games } from "@/lib/games/catalog";
import { tournamentFormatLabel, tournamentHref, tournamentStatusLabel, useTournaments } from "@/lib/query/tournaments";

export function TournamentStatusBadge({ status }: { status: TournamentStatus }) {
  const variant = status === TournamentStatus.RUNNING || status === TournamentStatus.SEEDING ? "signal"
    : status === TournamentStatus.REGISTRATION ? "success"
    : status === TournamentStatus.CANCELLED ? "destructive" : "secondary";
  return <Badge variant={variant} dot data-tournament-status={TournamentStatus[status].toLowerCase()}>{tournamentStatusLabel(status)}</Badge>;
}

function Row({ tournament }: { tournament: Tournament }) {
  const kind = gameKey(tournament.game);
  return (
    <ListItem
      href={tournamentHref(tournament.id)}
      className="mx-0"
      leading={<GameIcon game={kind} />}
      title={tournament.name}
      meta={<TournamentStatusBadge status={tournament.status} />}
      subtitle={`${games[kind].title} · ${tournamentFormatLabel(tournament.format)} · ${tournament.playerCount}/${tournament.capacity} players`}
    />
  );
}

export function TournamentsPage() {
  const list = useTournaments();
  const tournaments = list.data ?? [];
  return (
    <Pane title="Tournaments" back="/games" backLabel="Back to games" actions={<CreateTournamentDialog />}>
      {list.isPending ? (
        <div className="space-y-2">{[0, 1, 2].map(i => <Skeleton key={i} className="h-[52px] rounded-[10px]" />)}</div>
      ) : list.isError ? (
        <ErrorCard title="Could not load tournaments" error={list.error} onRetry={() => void list.refetch()} retrying={list.isFetching} />
      ) : tournaments.length ? (
        <nav aria-label="Tournaments" className="flex flex-col gap-0.5">
          {tournaments.map(tournament => <Row key={tournament.id} tournament={tournament} />)}
        </nav>
      ) : (
        <EmptyState icon={Trophy} title="No tournaments yet" />
      )}
    </Pane>
  );
}
