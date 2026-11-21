import { legalMoves, rankOf, suitOf, type DurakPosition } from "@/lib/games/durak";

function cost(card: string, trump: string) {
  return (suitOf(card) === suitOf(trump) ? 100 : 0) + rankOf(card);
}

export function botMove(position: DurakPosition): string | undefined {
  const legal = legalMoves(position);
  if (!legal.length) return undefined;
  const trump = position.trump;
  const cheapest = (moves: string[]) => moves.reduce((best, move) => cost(move.split(" ").at(-1)!, trump) < cost(best.split(" ").at(-1)!, trump) ? move : best);
  const defences = legal.filter(move => move.startsWith("defend "));
  if (defences.length) return cheapest(defences);
  if (legal.includes("take")) return "take";
  const attacks = legal.filter(move => move.startsWith("attack "));
  if (!attacks.length) return legal[0];
  if (position.table.length === 0) return cheapest(attacks);
  const throwable = attacks.filter(move => suitOf(move.slice(-2)) !== suitOf(trump) && rankOf(move.slice(-2)) <= rankOf("J"));
  if (throwable.length && (position.taking || position.stock.length > 0)) return cheapest(throwable);
  return legal.includes("pass") ? "pass" : cheapest(attacks);
}
