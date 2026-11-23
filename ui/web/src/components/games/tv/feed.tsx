"use client";

import { ChevronDown, ChevronUp, Tv } from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState, type UIEvent } from "react";

import { GameIcon } from "@/components/games/catalogue";
import { TvSlide } from "@/components/games/tv/slide";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { PaneHeader } from "@/components/ui/pane";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useSession } from "@/lib/auth/session";
import { useDebouncedValue } from "@/lib/hooks/use-debounced-value";
import { gameKey, games } from "@/lib/games/catalog";
import { finished, latest, nextLive, watchWindow } from "@/lib/games/tv";
import { useLiveFeed } from "@/lib/query/games";
import { cn } from "@/lib/utils";
import { useFeedSocket } from "@/lib/ws/use-feed-socket";

const stepKeys: Record<string, number> = { ArrowDown: 1, PageDown: 1, j: 1, ArrowUp: -1, PageUp: -1, k: -1 };

function typing(target: EventTarget | null) {
  return target instanceof HTMLElement && (target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName));
}

export function GamesTv({ embedded = false }: { embedded?: boolean }) {
  const { user } = useSession();
  const live = useLiveFeed(user?.id);
  const order = useMemo(() => live.data?.order ?? [], [live.data]);
  const viewport = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState(0);
  const current = Math.min(active, Math.max(0, order.length - 1));
  const settled = useDebouncedValue(current, 250);
  const watched = useMemo(() => watchWindow(order, Math.min(settled, Math.max(0, order.length - 1))), [order, settled]);
  const { frames } = useFeedSocket(watched);
  const sessionOf = useCallback((id: string) => latest(live.data?.sessions.get(id), frames.get(id)?.game), [frames, live.data]);
  const playing = order.filter(id => { const game = sessionOf(id); return game && !finished(game); }).length;

  const go = useCallback((index: number) => {
    const element = viewport.current;
    if (!element || index < 0 || index >= order.length) return;
    const smooth = !window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    element.scrollTo({ top: index * element.clientHeight, behavior: smooth ? "smooth" : "auto" });
  }, [order.length]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const step = stepKeys[event.key];
      if (!step || event.metaKey || event.ctrlKey || event.altKey || event.defaultPrevented || typing(event.target)) return;
      event.preventDefault();
      go(current + step);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [current, go]);

  const onScroll = (event: UIEvent<HTMLDivElement>) => {
    const element = event.currentTarget;
    if (element.clientHeight > 0) setActive(Math.round(element.scrollTop / element.clientHeight));
  };

  const header = embedded ? null : (
    <PaneHeader back="/games" backLabel="Back to games" leading={<span aria-hidden className="flex size-9 items-center justify-center rounded-[10px] bg-accent text-signal"><Tv className="size-[55%]" strokeWidth={2} /></span>}
      title="Axon TV" subtitle={live.isSuccess ? `${playing} live` : undefined} />
  );

  if (!live.data) {
    return (
      <div className="flex min-h-0 flex-1 flex-col bg-game-workspace">
        {header}
        <div className="grid flex-1 place-items-center p-6">
          {live.isError ? (
            <div className="flex flex-col items-center gap-3">
              <p role="alert" className="text-sm text-destructive">Could not load live games.</p>
              <Button variant="outline" className="min-h-11" onClick={() => void live.refetch()}>Try again</Button>
            </div>
          ) : <p role="status" className="text-sm text-muted-foreground">Loading live games…</p>}
        </div>
      </div>
    );
  }

  if (!order.length) {
    return (
      <div className="flex min-h-0 flex-1 flex-col bg-game-workspace">
        {header}
        <div data-tv-empty className="grid flex-1 place-items-center">
          <EmptyState icon={Tv} title="No live games right now">
            {embedded ? null : <Link href="/games" className={cn(buttonVariants({ variant: "signal" }), "min-h-11")}>Play a game</Link>}
          </EmptyState>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-game-workspace">
      {header}
      <div className="relative flex min-h-0 flex-1 flex-col">
        <ScrollArea className="min-h-0 flex-1" viewportProps={{
          ref: viewport,
          onScroll,
          tabIndex: 0,
          "aria-label": "Live games",
          className: "snap-y snap-mandatory overscroll-contain [container-type:size]",
        }}>
          {order.map((id, index) => {
            const game = sessionOf(id);
            const near = Math.abs(index - current) <= 1;
            const kind = gameKey(game?.game ?? "");
            const frame = frames.get(id);
            const following = nextLive(order, sessionOf, index);
            return (
              <section key={id} data-tv-slide={id} data-tv-game={game?.game} data-active={index === current || undefined}
                aria-roledescription="slide" aria-label={`${games[kind].title}, game ${index + 1} of ${order.length}`}
                className="h-[100cqh] snap-start snap-always">
                {game && near ? (
                  <TvSlide game={game} previous={frame?.previous} receivedAt={frame?.at ?? live.dataUpdatedAt} active={index === current} embedded={embedded}
                    next={following >= 0 ? () => go(following) : undefined} />
                ) : <div aria-hidden className="grid h-full place-items-center"><GameIcon game={kind} className="size-12 opacity-40" /></div>}
              </section>
            );
          })}
        </ScrollArea>
        <div className="pointer-events-none absolute inset-y-0 right-3 hidden flex-col items-center justify-center gap-2 md:flex">
          <Button variant="outline" size="icon" aria-label="Previous game" disabled={current === 0} onClick={() => go(current - 1)}
            className="pointer-events-auto size-11 rounded-full text-foreground shadow-sm"><ChevronUp aria-hidden /></Button>
          <span aria-hidden data-tv-position className="font-mono text-[12px] text-muted-foreground tabular-nums">{current + 1}/{order.length}</span>
          <Button variant="outline" size="icon" aria-label="Next game" disabled={current >= order.length - 1} onClick={() => go(current + 1)}
            className="pointer-events-auto size-11 rounded-full text-foreground shadow-sm"><ChevronDown aria-hidden /></Button>
        </div>
      </div>
    </div>
  );
}
