"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from "lucide-react";
import { useEffect, useState } from "react";

import { AnalysisPanel, EvalBar } from "@/components/games/chess/analysis";
import { ChessBoard } from "@/components/games/chess/board";
import { MoveList } from "@/components/games/chess/moves";
import { Side } from "@/components/games/chess/pieces";
import { QuoridorBoard, seatNames } from "@/components/games/quoridor/board";
import { ArenaHeader, iconClass } from "@/components/games/tictactoe/arena";
import { Board } from "@/components/games/tictactoe/board";
import { spectatorStatus } from "@/components/games/watch";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { AnalysisStatus } from "@/gen/axon/analysis/v1/analysis_pb";
import { SessionStatus, type HistoryFrame } from "@/gen/axon/game/v1/game_pb";
import { profileName } from "@/lib/format";
import { moveMarks } from "@/lib/games/analysis";
import { GameKey, gameHref, games } from "@/lib/games/catalog";
import { useAnalysis } from "@/lib/query/analysis";
import { useSessionHistory } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";

const sides: Record<GameKey, string[]> = {
  [GameKey.TicTacToe]: ["X", "O"],
  [GameKey.Chess]: ["White", "Black"],
  [GameKey.Quoridor]: [...seatNames],
};

function changedCell(frame: HistoryFrame, previous?: HistoryFrame) {
  const before = previous?.ticTacToe?.cells ?? [];
  const index = frame.ticTacToe?.cells.findIndex((cell, at) => cell !== before[at]) ?? -1;
  return index < 0 ? undefined : index;
}

function Position({ game, frame, previous }: { game: GameKey; frame: HistoryFrame; previous?: HistoryFrame }) {
  switch (game) {
    case GameKey.Chess:
      return <ChessBoard fen={frame.chess?.fen ?? ""} orientation={Side.White} legal={[]} lastMove={frame.chess?.lastMove} check={frame.chess?.check ?? false} playable={false} onMove={() => undefined} />;
    case GameKey.Quoridor:
      return <QuoridorBoard pawns={frame.quoridor?.pawns ?? ["e1", "e9"]} walls={frame.quoridor?.walls ?? []} seat={0} legal={[]} lastMove={frame.quoridor?.lastMove} playable={false} onMove={() => undefined} />;
    default:
      return <Board board={frame.ticTacToe} playable={false} move={() => undefined} lastMove={changedCell(frame, previous)} />;
  }
}

function notation(game: GameKey, frames: HistoryFrame[]) {
  if (game === GameKey.TicTacToe) return [];
  return frames.slice(1).map(frame => frame.move ?? "");
}

export function GameReplay({ id, game }: { id: string; game: GameKey }) {
  const history = useSessionHistory(id);
  const frames = history.data?.frames ?? [];
  const session = history.data?.session;
  const last = Math.max(0, frames.length - 1);
  const [cursor, setCursor] = useState<number>();
  const at = Math.min(cursor ?? last, last);
  const profiles = usePublicProfiles(session?.playerIds ?? []);
  const names = (session?.playerIds ?? []).map(player => profileName(profiles.data?.get(player)));
  const step = (to: number) => setCursor(Math.max(0, Math.min(last, to)));
  const analysed = game === GameKey.Chess && !!session?.rated && (session.status === SessionStatus.WON || session.status === SessionStatus.DRAW);
  const analysis = useAnalysis(id, analysed);
  const ready = analysis.data?.status === AnalysisStatus.READY ? analysis.data : undefined;

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return;
      const next = { ArrowLeft: at - 1, ArrowRight: at + 1, Home: 0, End: last }[event.key];
      if (next === undefined) return;
      event.preventDefault();
      setCursor(Math.max(0, Math.min(last, next)));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [at, last]);

  const code = ConnectError.from(history.error).code;
  const frame = frames[at];
  const played = notation(game, frames);
  const moves = played.slice(0, at);
  const mover = frame?.playerId ? session?.playerIds.indexOf(frame.playerId) ?? -1 : -1;
  const result = session ? spectatorStatus(session, names, sides[game]) : "";
  const marks = ready ? moveMarks(ready.positions).slice(0, at) : undefined;
  const board = frame ? <Position game={game} frame={frame} previous={frames[at - 1]} /> : null;

  return (
    <ScrollArea className="min-h-0 flex-1 bg-game-workspace" viewportProps={{ className: "[&>div]:min-h-full" }}>
      <main className="flex min-h-full w-full flex-col px-[max(16px,env(safe-area-inset-left))] pt-[max(16px,env(safe-area-inset-top))] pb-[max(16px,env(safe-area-inset-bottom))]">
        {!history.data ? (
          <section className="max-w-md space-y-4 rounded-2xl border border-border bg-card p-5">
            {history.isPending ? <p role="status">Loading replay…</p> : <>
              <p role="alert">{code === Code.NotFound ? "This game has no replay yet." : "Could not load the replay."}</p>
              {code !== Code.NotFound ? <Button variant="outline" className="min-h-11" onClick={() => void history.refetch()}>Try again</Button> : null}
            </>}
          </section>
        ) : (
          <div className="mx-auto grid w-full max-w-[1040px] min-w-0 gap-4 font-sans text-[14px] leading-[1.45] text-game-text lg:grid-cols-[minmax(0,620px)_minmax(260px,1fr)]">
            <section aria-labelledby="replay-title" className="grid min-w-0 gap-3">
              <ArenaHeader title={`${games[game].title} replay`} lobby={gameHref(id, game)}>{null}</ArenaHeader>
              <h2 id="replay-title" className="sr-only">Replay board</h2>
              <div className="relative">{analysed && board ? <div className="flex items-stretch gap-2"><EvalBar position={ready?.positions[at]} /><div className="min-w-0 flex-1">{board}</div></div> : board}</div>
              <div className="grid gap-2 rounded-game-md border border-game-border bg-game-card p-3">
                <div className="flex items-center justify-between gap-2">
                  <p role="status" aria-live="polite" data-replay-ply={at} className="text-sm font-medium">
                    {at === 0 ? "Start position" : `Move ${at} of ${last}`}
                    {mover >= 0 ? <span className="text-game-text2">{` · ${names[mover] || sides[game][mover]}`}</span> : null}
                  </p>
                  <div className="flex items-center">
                    <button type="button" aria-label="First move" className={iconClass} disabled={at === 0} onClick={() => step(0)}><ChevronsLeft aria-hidden /></button>
                    <button type="button" aria-label="Previous move" className={iconClass} disabled={at === 0} onClick={() => step(at - 1)}><ChevronLeft aria-hidden /></button>
                    <button type="button" aria-label="Next move" className={iconClass} disabled={at === last} onClick={() => step(at + 1)}><ChevronRight aria-hidden /></button>
                    <button type="button" aria-label="Last move" className={iconClass} disabled={at === last} onClick={() => step(last)}><ChevronsRight aria-hidden /></button>
                  </div>
                </div>
                <input type="range" aria-label="Move" min={0} max={last} value={at} onChange={event => step(Number(event.target.value))} className="h-11 w-full accent-game-primary" />
              </div>
            </section>
            <aside className="flex min-w-0 flex-col gap-3 lg:pt-14">
              {result ? <p data-replay-result className="rounded-game-md border border-game-border bg-game-card px-3 py-2 text-sm font-medium">{result}</p> : null}
              {analysed ? <AnalysisPanel report={analysis.data} failed={analysis.isError} retry={() => void analysis.refetch()} at={at} onSelect={step}
                san={played} fens={frames.map(item => item.chess?.fen ?? "")} /> : null}
              {game !== GameKey.TicTacToe ? <MoveList san={moves} marks={marks} /> : null}
            </aside>
          </div>
        )}
      </main>
    </ScrollArea>
  );
}
