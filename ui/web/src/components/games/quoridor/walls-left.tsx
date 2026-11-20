import { wallsEach } from "@/lib/games/quoridor";
import { cn } from "@/lib/utils";

export function WallsLeft({ count, seat }: { count: number; seat: number }) {
  return (
    <span data-walls-left={count} aria-label={`${count} walls left`} className="flex items-center gap-[3px]">
      {Array.from({ length: wallsEach }, (_, index) => (
        <span key={index} aria-hidden className={cn("h-3 w-[3px] rounded-full", index < count ? "bg-quoridor-wall" : "bg-game-border")} />
      ))}
      <span aria-hidden className="ml-1 font-mono text-[12px] text-game-text2" data-seat={seat}>{count}</span>
    </span>
  );
}
