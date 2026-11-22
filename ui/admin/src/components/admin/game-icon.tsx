import { BrickWall, ChessKnight, Circle, CircleDot, Gamepad2, Spade, X, type LucideIcon } from "lucide-react";

import { GameKey, gameKeys, games } from "@/lib/games/catalog";
import { cn } from "@/lib/utils";

const glyphs: Partial<Record<string, LucideIcon>> = {
  [GameKey.Chess]: ChessKnight,
  [GameKey.Quoridor]: BrickWall,
  [GameKey.Checkers]: CircleDot,
  [GameKey.Durak]: Spade,
};

export function GameIcon({ game, className }: { game: string; className?: string }) {
  const tile = cn("flex shrink-0 items-center justify-center rounded-[9px] bg-accent text-signal size-7", className);
  if (game === GameKey.TicTacToe) {
    return (
      <span aria-hidden className={cn(tile, "gap-px")}>
        <X className="size-[44%]" strokeWidth={2.75} />
        <Circle className="size-[31%] text-success" strokeWidth={3} />
      </span>
    );
  }
  const Glyph = glyphs[game] ?? Gamepad2;
  return (
    <span aria-hidden className={cn(tile, !glyphs[game] && "text-muted-foreground")}>
      <Glyph className="size-[55%]" strokeWidth={2} />
    </span>
  );
}

export function gameName(game: string): string {
  const key = gameKeys.find((candidate) => candidate === game);
  return key ? games[key].title : game;
}

export function GameName({ game, className, iconClassName }: { game: string; className?: string; iconClassName?: string }) {
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-2.5", className)}>
      <GameIcon game={game} className={iconClassName} />
      <span className="truncate">{gameName(game)}</span>
    </span>
  );
}
