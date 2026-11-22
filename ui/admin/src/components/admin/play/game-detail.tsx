"use client";

import { Code } from "@connectrpc/connect";
import { ExternalLink, Gamepad2, Square } from "lucide-react";
import Link from "next/link";
import { Fragment, useMemo, useState } from "react";

import { AuditList } from "@/components/admin/audit-table";
import { gameTitle, Mono, Person, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { CopyID, Fact, Facts, RecordEmpty, RecordFrame, RecordIcon, RecordLoading, RecordPage, RecordSection, RecordState } from "@/components/admin/record";
import { finished, SessionStatusBadge } from "@/components/admin/games-table";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { isCode } from "@/lib/errors";
import { gameHref } from "@/lib/games/catalog";
import { useAdminProfiles, useCancelGame } from "@/lib/query/admin";
import { useAdminGame } from "@/lib/query/play";

const back = { section: "Games", back: "/games", backLabel: "Back to games" };

export function GameDetail({ id }: { id: string }) {
  const access = useAdminAccess();
  const game = useAdminGame(id);
  const cancel = useCancelGame();
  const [stopping, setStopping] = useState(false);
  const session = game.data?.session;
  const profiles = useAdminProfiles(useMemo(() => session?.playerIds ?? [], [session]));

  if (game.isPending) return <RecordLoading {...back} label="Loading game" />;
  if (game.isError || !session) {
    return (
      <RecordState {...back}>
        {isCode(game.error, Code.NotFound) || !session ? <EmptyState icon={Gamepad2} title="This game does not exist" /> : <ErrorCard title="Could not load the game" error={game.error} onRetry={() => void game.refetch()} />}
      </RecordState>
    );
  }

  const moves = game.data.moves;
  const done = finished(session);
  const stoppable = access.can(Permission.GamesModerate) && !done && !session.tournamentId;

  return (
    <RecordPage
      {...back}
      media={<RecordIcon icon={Gamepad2} tone={done ? "muted" : "signal"} />}
      title={`${gameTitle(session.game)} game`}
      lines={session.playerIds.length ? (
        <span className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 text-foreground">
          {session.playerIds.map((player, i) => (
            <Fragment key={player}>
              {i > 0 ? <span className="text-xs text-muted-foreground">vs</span> : null}
              <Person id={player} profiles={profiles.data} />
            </Fragment>
          ))}
        </span>
      ) : <span>Waiting for players</span>}
      tags={
        <>
          <SessionStatusBadge session={session} />
          <Badge>{session.rated ? "Rated" : "Casual"}</Badge>
          {session.tournamentId ? <Link href={`/tournaments/${session.tournamentId}`}><Badge variant="outline" className="hover:bg-muted">Tournament game</Badge></Link> : null}
        </>
      }
      actions={
        <>
          <a href={gameHref(session.id, session.game)} className={buttonVariants({ variant: "ghost", size: "sm" })}><ExternalLink aria-hidden /> Open in Axon</a>
          {stoppable ? <Button variant="destructive-outline" size="sm" onClick={() => setStopping(true)}><Square aria-hidden /> Stop</Button> : null}
        </>
      }
      facts={
        <Facts>
          <Fact term="Winner"><Person id={session.winnerUserId} profiles={profiles.data} /></Fact>
          <Fact term="Moves"><span className="tabular-nums">{session.ply}</span></Fact>
          <Fact term="Watching"><span className="tabular-nums">{session.spectators ?? 0}</span></Fact>
          <Fact term="Updated"><When at={session.updatedAt} /></Fact>
          {session.tournamentId ? <Fact term="Tournament"><Link href={`/tournaments/${session.tournamentId}`} className="hover:underline"><Mono>{session.tournamentId.slice(0, 8)}</Mono></Link></Fact> : null}
          <Fact term="Session ID"><CopyID value={session.id} label="session ID" /></Fact>
        </Facts>
      }
    >
      <RecordSection id="moves" title="Moves" count={moves.length}>
        <RecordFrame>
          {moves.length === 0 ? <RecordEmpty>No moves yet</RecordEmpty> : (
            <ScrollArea className="max-h-[28rem]" orientation="both">
              <table aria-label="Moves" className="w-full min-w-[32rem] text-left text-sm">
                <thead className="sticky top-0 z-[1] bg-surface-raised text-xs text-muted-foreground">
                  <tr>
                    <th scope="col" className="w-16 px-4 py-2.5 text-right font-medium">Ply</th>
                    <th scope="col" className="px-4 py-2.5 font-medium">Player</th>
                    <th scope="col" className="px-4 py-2.5 font-medium">Move</th>
                    <th scope="col" className="px-4 py-2.5 font-medium">Played</th>
                  </tr>
                </thead>
                <tbody>
                  {moves.map((move) => (
                    <tr key={move.ply} className="border-t border-line">
                      <td className="px-4 py-2 text-right tabular-nums text-muted-foreground">{move.ply}</td>
                      <td className="px-4 py-2"><Person id={move.playerId} profiles={profiles.data} linked={false} /></td>
                      <td className="max-w-[24rem] truncate px-4 py-2 font-mono text-xs" title={move.data}>{move.data}</td>
                      <td className="px-4 py-2 text-muted-foreground"><When at={move.playedAt} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </ScrollArea>
          )}
        </RecordFrame>
      </RecordSection>

      {access.can(Permission.AuditRead) ? (
        <RecordSection id="history" title="History" aside={<Link className={buttonVariants({ variant: "link", size: "sm" })} href={`/audit?f.target_type=game_session&f.target=${session.id}`}>Open in audit log</Link>}>
          <AuditList filters={{ targetType: "game_session", target: session.id }} />
        </RecordSection>
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
    </RecordPage>
  );
}
