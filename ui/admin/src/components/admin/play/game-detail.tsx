"use client";

import { Code } from "@connectrpc/connect";
import { ExternalLink, Gamepad2, Square } from "lucide-react";
import Link from "next/link";
import { useMemo, useState, type ReactNode } from "react";

import { AuditList } from "@/components/admin/audit-table";
import { gameTitle, Mono, Person, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { finished, SessionStatusBadge } from "@/components/admin/games-table";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { Pane, PaneSection } from "@/components/ui/pane";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { isCode } from "@/lib/errors";
import { gameHref } from "@/lib/games/catalog";
import { useAdminProfiles, useCancelGame } from "@/lib/query/admin";
import { useAdminGame } from "@/lib/query/play";

function Fact({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{term}</dt>
      <dd className="min-w-0 text-sm break-words">{children}</dd>
    </div>
  );
}

export function GameDetail({ id }: { id: string }) {
  const access = useAdminAccess();
  const game = useAdminGame(id);
  const cancel = useCancelGame();
  const [stopping, setStopping] = useState(false);
  const session = game.data?.session;
  const profiles = useAdminProfiles(useMemo(() => session?.playerIds ?? [], [session]));

  if (game.isPending) {
    return <Pane title="Game" back="/games" width="wide"><div role="status" aria-label="Loading game" className="space-y-3"><Skeleton className="h-28 w-full" /><Skeleton className="h-40 w-full" /></div></Pane>;
  }
  if (game.isError || !session) {
    return (
      <Pane title="Game" back="/games" width="wide">
        {isCode(game.error, Code.NotFound) || !session ? <EmptyState icon={Gamepad2} title="This game does not exist" /> : <ErrorCard title="Could not load the game" error={game.error} onRetry={() => void game.refetch()} />}
      </Pane>
    );
  }

  const moves = game.data.moves;
  const stoppable = access.can(Permission.GamesModerate) && !finished(session) && !session.tournamentId;

  return (
    <Pane
      title={`${gameTitle(session.game)} game`}
      back="/games"
      backLabel="Back to games"
      width="wide"
      actions={
        <>
          <a href={gameHref(session.id, session.game)} className={buttonVariants({ variant: "ghost", size: "sm" })}><ExternalLink aria-hidden /> Open in Axon</a>
          {stoppable ? <Button variant="destructive-outline" size="sm" onClick={() => setStopping(true)}><Square aria-hidden /> Stop</Button> : null}
        </>
      }
    >
      <PaneSection id="session" title="Session" aside={<SessionStatusBadge session={session} />}>
        <dl className="grid grid-cols-[repeat(auto-fill,minmax(12rem,1fr))] gap-x-6 gap-y-3">
          {session.playerIds.map((player, i) => <Fact key={player} term={`Player ${i + 1}`}><Person id={player} profiles={profiles.data} /></Fact>)}
          <Fact term="Winner"><Person id={session.winnerUserId} profiles={profiles.data} /></Fact>
          <Fact term="Moves"><span className="tabular-nums">{session.ply}</span></Fact>
          <Fact term="Rated">{session.rated ? "yes" : "no"}</Fact>
          <Fact term="Watching"><span className="tabular-nums">{session.spectators ?? 0}</span></Fact>
          <Fact term="Updated"><When at={session.updatedAt} /></Fact>
          {session.tournamentId ? <Fact term="Tournament"><Link href={`/tournaments/${session.tournamentId}`} className="hover:underline"><Mono>{session.tournamentId}</Mono></Link></Fact> : null}
          <Fact term="Session ID"><Mono>{session.id}</Mono></Fact>
        </dl>
      </PaneSection>

      <PaneSection id="moves" title={`Moves · ${moves.length}`}>
        {moves.length === 0 ? <p className="text-sm text-muted-foreground">No moves yet</p> : (
          <ScrollArea className="max-h-[28rem]" orientation="both">
            <table aria-label="Moves" className="w-full min-w-[32rem] text-left text-sm">
              <thead className="sticky top-0 bg-surface text-xs text-muted-foreground">
                <tr><th scope="col" className="py-2 pr-3 font-medium">Ply</th><th scope="col" className="py-2 pr-3 font-medium">Player</th><th scope="col" className="py-2 pr-3 font-medium">Move</th><th scope="col" className="py-2 font-medium">Played</th></tr>
              </thead>
              <tbody>
                {moves.map((move) => (
                  <tr key={move.ply} className="border-t border-line">
                    <td className="py-1.5 pr-3 tabular-nums">{move.ply}</td>
                    <td className="py-1.5 pr-3"><Person id={move.playerId} profiles={profiles.data} linked={false} /></td>
                    <td className="max-w-[24rem] truncate py-1.5 pr-3" title={move.data}><Mono>{move.data}</Mono></td>
                    <td className="py-1.5"><When at={move.playedAt} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </ScrollArea>
        )}
      </PaneSection>

      {access.can(Permission.AuditRead) ? (
        <PaneSection id="history" title="History">
          <AuditList filters={{ targetType: "game_session", target: session.id }} />
        </PaneSection>
      ) : null}

      <ConfirmAction
        open={stopping}
        onOpenChange={setStopping}
        title={`Stop this ${gameTitle(session.game)} game?`}
        consequence="The game ends without a result and its players return to the lobby. Ratings do not change."
        confirmLabel="Stop game"
        onConfirm={async (reason) => {
          await cancel.mutateAsync({ sessionId: session.id, reason });
          await game.refetch();
        }}
      />
    </Pane>
  );
}
