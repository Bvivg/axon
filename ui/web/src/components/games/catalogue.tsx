"use client";

import { ArrowRight, ChevronRight, Circle, X } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";

import { SidebarSearchField } from "@/components/chat/sidebar-search-field";
import { ScrollArea } from "@/components/ui/scroll-area";

export const ticTacToeLobbyHref = "/games/tic-tac-toe";

export function GameEntry() {
  const selected = usePathname().startsWith(ticTacToeLobbyHref);
  return (
    <Link href={ticTacToeLobbyHref} aria-label="Tic-Tac-Toe lobby" aria-current={selected ? "page" : undefined} className="game-directory-entry">
      <span aria-hidden className="game-directory-icon"><X size={25} strokeWidth={2.5} /><Circle size={17} strokeWidth={2.5} /></span>
      <span className="min-w-0 flex-1"><strong>Tic-Tac-Toe</strong><span>2 players · Quick match</span></span>
      <ChevronRight aria-hidden size={17} />
    </Link>
  );
}

export function GamesSidebar() {
  const [query, setQuery] = useState("");
  const matches = "tic-tac-toe".includes(query.trim().toLowerCase());
  return (
    <>
      <h1 className="workspace-context-heading">Games</h1>
      <SidebarSearchField label="Find a game" value={query} onChange={setQuery} />
      <div className="workspace-context-label">Pick your game</div>
      <nav aria-label="Game catalogue" className="space-y-1">{matches ? <GameEntry /> : <p className="px-3 py-4 text-xs text-muted-foreground">No games found.</p>}</nav>
    </>
  );
}

export function GameArtwork({ className = "" }: { className?: string }) {
  return <div aria-hidden className={`games-artwork ${className}`}>
    <div className="games-artwork-board"><X /><Circle /><span /><span /><X /><Circle /><Circle /><span /><X /></div>
    <span className="games-artwork-spark games-artwork-spark-one" />
    <span className="games-artwork-spark games-artwork-spark-two" />
  </div>;
}

export function GamesCatalogue() {
  return (
    <ScrollArea className="min-h-0 flex-1">
      <main className="workspace-page games-catalogue">
        <span className="games-eyebrow">One place. More connection.</span>
        <h1 className="games-catalogue-title">Pick a game.<br />Bring your people.</h1>
        <section className="games-feature" aria-labelledby="games-welcome-title">
          <div><span className="games-eyebrow">GAME NIGHT / AXON</span><h2 id="games-welcome-title">A little friendly rivalry.</h2><p>Enter the lobby, invite a friend, and make your move.</p><Link href={ticTacToeLobbyHref}>Enter Tic-Tac-Toe lobby<ArrowRight aria-hidden size={17} /></Link></div>
          <GameArtwork />
        </section>
        <h2 className="game-section-title mt-8">Games in AXON</h2>
        <nav aria-label="Choose a game" className="mt-3"><GameEntry /></nav>
      </main>
    </ScrollArea>
  );
}
