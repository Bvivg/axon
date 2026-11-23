import { Shield, Swords } from "lucide-react";

import { ScrollArea } from "@/components/ui/scroll-area";
import { rankLabel, suitOf, suitSymbols } from "@/lib/games/durak";
import { cn } from "@/lib/utils";

const short = (card: string) => `${rankLabel(card)}${suitSymbols[suitOf(card)] ?? ""}`;

export function moveLabel(move: string) {
  const [action, first, second] = move.split(" ");
  if (action === "attack" && first) return short(first);
  if (action === "defend" && first && second) return `${short(first)} / ${short(second)}`;
  if (action === "transfer" && first) return `→ ${short(first)}`;
  if (action === "catch" && first) return `Catch ${short(first)}`;
  return action === "take" ? "Take" : action === "pass" ? "Pass" : move;
}

export function RoleBadge({ attacking, className }: { attacking: boolean; className?: string }) {
  const Icon = attacking ? Swords : Shield;
  return <Icon aria-hidden className={cn("size-[11px] text-game-text2", className)} strokeWidth={2.25} />;
}

export function DurakMoves({ moves }: { moves: string[] }) {
  return (
    <section aria-labelledby="durak-moves-title" className="flex min-h-0 flex-col rounded-game-md border border-game-border bg-game-card">
      <h2 id="durak-moves-title" className="border-b border-game-border px-3 py-2 text-[13px] font-semibold">Moves</h2>
      <ScrollArea className="max-h-48 min-h-0 lg:max-h-[420px]">
        {moves.length ? (
          <ol data-testid="move-list" className="grid grid-cols-[2.25rem_1fr] gap-y-0.5 p-2 font-mono text-[13px]">
            {moves.map((move, index) => (
              <li key={index} className="contents">
                <span className="text-game-text2">{index + 1}.</span>
                <span data-move={move}>{moveLabel(move)}</span>
              </li>
            ))}
          </ol>
        ) : <p className="p-3 text-[13px] text-game-text2">No moves yet</p>}
      </ScrollArea>
    </section>
  );
}
