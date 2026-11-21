import { isRed, rankLabel, suitOf, suitSymbols } from "@/lib/games/durak";
import { cn } from "@/lib/utils";

export const cardSize = "w-[clamp(40px,11vw,60px)] aspect-[5/7]";

export function CardFace({ card, className }: { card: string; className?: string }) {
  const symbol = suitSymbols[suitOf(card)] ?? "?";
  return (
    <span aria-hidden data-card={card} className={cn("relative grid shrink-0 place-items-center rounded-[14%] border border-game-border bg-durak-face font-sans shadow-[0_1px_2px_rgb(0_0_0/.18)] select-none",
      isRed(card) ? "text-durak-red" : "text-durak-ink", cardSize, className)}>
      <span className="absolute top-[6%] left-[10%] flex flex-col items-center text-[clamp(11px,3vw,15px)] leading-none font-semibold">{rankLabel(card)}<span className="text-[0.85em]">{symbol}</span></span>
      <span className="text-[clamp(18px,5vw,28px)] leading-none">{symbol}</span>
    </span>
  );
}

export function CardBack({ className }: { className?: string }) {
  return (
    <span aria-hidden data-card-back className={cn("block shrink-0 rounded-[14%] border-2 border-durak-face bg-durak-back bg-[repeating-linear-gradient(45deg,var(--color-durak-back-line)_0_2px,transparent_2px_7px)] shadow-[0_1px_2px_rgb(0_0_0/.25)]", cardSize, className)} />
  );
}
