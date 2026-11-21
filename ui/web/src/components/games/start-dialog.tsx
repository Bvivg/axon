"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Cpu, Link2, Loader2, Play, Swords } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { DurakTableSettings, defaultDurakSettings, describeDurak, durakOptions, type DurakSettings } from "@/components/games/durak/table-settings";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { MatchTicket } from "@/gen/axon/game/v1/game_pb";
import { gameClient } from "@/lib/connect/clients";
import { fromTimestamp } from "@/lib/format";
import { GameKey, practiceHref } from "@/lib/games/catalog";
import { gameHref, sessionKey, sessionsKey } from "@/lib/query/games";
import { useMatchSearch } from "@/lib/ws/use-match-search";

const optionClass = "flex w-full items-center gap-3 rounded-[14px] border border-line bg-surface p-3 text-start transition-colors hover:border-signal/50 hover:bg-accent disabled:opacity-60";

function Option({ icon, title, hint, children }: { icon: ReactNode; title: string; hint: string; children?: ReactNode }) {
  return (
    <>
      <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-accent text-signal">{icon}</span>
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-semibold">{title}</span>
        <span className="block text-xs text-muted-foreground">{hint}</span>
      </span>
      {children}
    </>
  );
}

export function StartGameDialog({ game, rating, autoQuick = false }: { game: GameKey; rating?: number; autoQuick?: boolean }) {
  const router = useRouter();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const requestID = useRef<string | undefined>(undefined);
  const [durak, setDurak] = useState<DurakSettings>(defaultDurakSettings);
  const table = useMemo(() => game === GameKey.Durak ? { seats: durak.seats, options: durakOptions(durak) } : {}, [game, durak]);
  const bots = game === GameKey.Durak ? durak.bots : 0;
  const withBots = bots > 0 && bots >= durak.seats - 1;
  const search = useMatchSearch(game, sessionId => {
    void client.invalidateQueries({ queryKey: sessionsKey });
    router.push(gameHref(sessionId, game));
  });
  const privateGame = useMutation({
    mutationFn: async () => {
      requestID.current ??= crypto.randomUUID();
      const { session } = await gameClient.createSession({ game, requestId: requestID.current, ...table, bots: bots || undefined });
      if (!session) throw new Error("Could not create the game.");
      return session;
    },
    onSuccess: session => {
      requestID.current = undefined;
      client.setQueryData(sessionKey(session.id), session);
      void client.invalidateQueries({ queryKey: sessionsKey });
      router.push(gameHref(session.id, session.game));
    },
  });
  const started = useRef(false);
  useEffect(() => {
    if (!autoQuick || started.current) return;
    started.current = true;
    setOpen(true);
    void search.start(table);
  }, [autoQuick, search, table]);
  const visible = open || search.searching;

  return (
    <>
      <Button variant="signal" className="min-h-11 w-full gap-2 sm:w-auto" onClick={() => setOpen(true)}>
        <Play aria-hidden className="size-4" />Start a game
      </Button>
      <Dialog open={visible} onOpenChange={next => { if (!search.searching) setOpen(next); }}>
        <DialogContent className="space-y-4">
          <DialogTitle>{search.searching ? "Finding an opponent" : "Start a game"}</DialogTitle>
          {search.searching && search.ticket ? (
            <Searching ticket={search.ticket} receivedAt={search.receivedAt} busy={search.busy} cancel={() => { void search.cancel(); setOpen(false); }} />
          ) : (
            <ScrollArea className="-mx-1 max-h-[calc(100dvh-9rem)]"><div className="space-y-2 px-1">
              {game === GameKey.Durak ? <DurakTableSettings value={durak} onChange={setDurak} withBots /> : null}
              <button type="button" className={optionClass} disabled={search.busy || bots > 0} onClick={() => void search.start(table)}>
                <Option icon={<Swords aria-hidden className="size-5" />} title="Quick match" hint={quickHint(game, durak, rating)}>
                  {search.busy ? <Loader2 aria-hidden className="size-4 animate-spin text-muted-foreground" /> : null}
                </Option>
              </button>
              <button type="button" className={optionClass} disabled={privateGame.isPending} onClick={() => privateGame.mutate()}>
                <Option icon={<Link2 aria-hidden className="size-5" />} title={privateGame.isPending ? "Creating game…" : withBots ? "Play the bots" : "Private game"} hint={withBots ? "Unrated · starts right away" : "Unrated · share the link with a friend"} />
              </button>
              <Link href={practiceHref(game)} className={optionClass}>
                <Option icon={<Cpu aria-hidden className="size-5" />} title="Practice offline" hint="Against the computer or on one device" />
              </Link>
              {search.error ? <p role="alert" className="text-sm text-destructive">{search.error}</p> : null}
              {privateGame.isError ? <p role="alert" className="text-sm text-destructive">Could not create the game. Try again.</p> : null}
            </div></ScrollArea>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function quickHint(game: GameKey, durak: DurakSettings, rating?: number): string {
  if (game === GameKey.Durak) {
    if (durak.bots > 0) return "Bots only join private tables";
    return `${durak.cheats ? "Unrated" : "Rated"} · ${describeDurak(durak)}`;
  }
  return rating ? `Rated · your rating ${rating}` : "Rated · find an opponent";
}

function Searching({ ticket, receivedAt, busy, cancel }: { ticket: MatchTicket; receivedAt: number; busy: boolean; cancel: () => void }) {
  const joined = fromTimestamp(ticket.joinedAt)?.getTime() ?? 0;
  const sampled = fromTimestamp(ticket.sampledAt)?.getTime() ?? joined;
  const [now, setNow] = useState(() => performance.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(performance.now()), 250);
    return () => clearInterval(timer);
  }, []);
  const seconds = Math.max(0, Math.floor((sampled - joined + Math.max(0, now - receivedAt)) / 1000));
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-3 rounded-[14px] border border-line bg-surface p-3">
        <Loader2 aria-hidden className="size-5 animate-spin text-signal" />
        <div className="min-w-0 flex-1 text-sm">
          <p className="font-semibold" role="status">Searching · <span className="font-mono">{`${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`}</span></p>
          <p className="text-xs text-muted-foreground">Rating {ticket.rating} · {ticket.window === undefined ? "any opponent" : `±${ticket.window}`}</p>
        </div>
      </div>
      <Button variant="outline" className="min-h-11 w-full" disabled={busy} onClick={cancel}>Cancel search</Button>
    </div>
  );
}
