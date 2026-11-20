import { ScrollArea } from "@/components/ui/scroll-area";

export function MoveList({ san }: { san: string[] }) {
  const pairs = Array.from({ length: Math.ceil(san.length / 2) }, (_, index) => [san[index * 2], san[index * 2 + 1]] as const);
  return (
    <section aria-labelledby="chess-moves-title" className="flex min-h-0 flex-col rounded-game-md border border-game-border bg-game-card">
      <h2 id="chess-moves-title" className="border-b border-game-border px-3 py-2 text-[13px] font-semibold">Moves</h2>
      <ScrollArea className="max-h-48 min-h-0 lg:max-h-[420px]">
        {pairs.length ? (
          <ol data-testid="move-list" className="grid grid-cols-[2.25rem_1fr_1fr] gap-y-0.5 p-2 font-mono text-[13px]">
            {pairs.map(([white, black], index) => (
              <li key={index} className="contents">
                <span className="text-game-text2">{index + 1}.</span>
                <span>{white}</span>
                <span>{black ?? ""}</span>
              </li>
            ))}
          </ol>
        ) : <p className="p-3 text-[13px] text-game-text2">No moves yet</p>}
      </ScrollArea>
    </section>
  );
}
