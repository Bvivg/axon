"use client";

import { LoaderCircle } from "lucide-react";
import type { MouseEvent } from "react";

import { Button } from "@/components/ui/button";
import { AnalysisStatus, MoveClass, type GetAnalysisResponse, type PositionAnalysis } from "@/gen/axon/analysis/v1/analysis_pb";
import { evaluationLabel, markClass, MoveMark, moveClassLabel, moveMark, sanOf, whiteShare } from "@/lib/games/analysis";
import { cn } from "@/lib/utils";

export function EvalBar({ position }: { position?: PositionAnalysis }) {
  const label = position ? evaluationLabel(position) : "";
  return (
    <div data-eval-bar data-eval={label || "pending"} role="img" aria-label={position ? `Evaluation ${label}` : "Evaluation pending"}
      className="flex w-3 shrink-0 flex-col overflow-hidden rounded-full border border-game-border bg-chess-piece-black sm:w-4">
      <div className="mt-auto w-full bg-chess-piece-white transition-[height] duration-300 motion-reduce:transition-none" style={{ height: `${(position ? whiteShare(position) : 0.5) * 100}%` }} />
    </div>
  );
}

export function EvalChart({ positions, at, onSelect }: { positions: PositionAnalysis[]; at: number; onSelect: (ply: number) => void }) {
  const last = Math.max(1, positions.length - 1);
  const heights = positions.map(position => (1 - whiteShare(position)) * 100);
  const area = `M0,100 ${heights.map((y, ply) => `L${ply},${y.toFixed(2)}`).join(" ")} L${positions.length - 1},100 Z`;
  const seek = (event: MouseEvent<SVGSVGElement>) => {
    const box = event.currentTarget.getBoundingClientRect();
    if (box.width > 0) onSelect(Math.round(((event.clientX - box.left) / box.width) * last));
  };
  return (
    <div className="relative h-16 w-full overflow-hidden rounded-game-sm border border-game-border bg-chess-piece-black">
      <svg data-eval-chart={positions.length} viewBox={`0 0 ${last} 100`} preserveAspectRatio="none" role="img" aria-label="Evaluation by move" className="absolute inset-0 size-full cursor-pointer" onClick={seek}>
        <path d={area} className="fill-chess-piece-white" />
        <line x1={0} x2={last} y1={50} y2={50} strokeDasharray="3 3" vectorEffect="non-scaling-stroke" className="stroke-game-text2 [stroke-width:1]" />
        <line x1={at} x2={at} y1={0} y2={100} vectorEffect="non-scaling-stroke" className="stroke-game-primary [stroke-width:2]" />
      </svg>
      {positions.map(position => {
        const mark = moveMark(position.moveClass);
        return mark ? <span key={position.ply} aria-hidden data-chart-mark={mark} className={cn("pointer-events-none absolute size-2 -translate-x-1/2 -translate-y-1/2 rounded-full border border-game-card", mark === MoveMark.Blunder ? "bg-game-error" : "bg-game-warning")}
          style={{ left: `${(position.ply / last) * 100}%`, top: `${heights[position.ply]}%` }} /> : null;
      })}
    </div>
  );
}

function Verdict({ positions, at, san, fens }: { positions: PositionAnalysis[]; at: number; san: string[]; fens: string[] }) {
  const position = positions[at];
  if (!position) return null;
  const mark = moveMark(position.moveClass);
  const best = at > 0 && position.moveClass !== MoveClass.BEST ? sanOf(fens[at - 1] ?? "", positions[at - 1]?.bestMove) : undefined;
  return (
    <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1 text-sm">
      <span data-analysis-eval className="font-mono text-[15px] font-semibold tabular-nums">{evaluationLabel(position)}</span>
      {at > 0 ? <span data-analysis-move={position.moveClass}>
        {san[at - 1]}{mark ? <span className={cn("font-semibold", markClass(mark))}>{mark}</span> : null}
        <span className="text-game-text2">{` · ${moveClassLabel(position.moveClass)}`}</span>
      </span> : <span className="text-game-text2">Start position</span>}
      {best ? <span className="text-game-text2">{`Best ${best}`}</span> : null}
    </div>
  );
}

export function AnalysisPanel({ report, failed, retry, at, onSelect, san, fens }: {
  report?: GetAnalysisResponse | null; failed: boolean; retry: () => void; at: number; onSelect: (ply: number) => void; san: string[]; fens: string[];
}) {
  const status = failed ? "error" : report?.status === AnalysisStatus.READY ? "ready" : report?.status === AnalysisStatus.FAILED ? "failed" : "pending";
  return (
    <section aria-labelledby="analysis-title" data-analysis={status} className="grid gap-2 rounded-game-md border border-game-border bg-game-card p-3">
      <div className="flex items-baseline justify-between gap-2">
        <h2 id="analysis-title" className="text-[13px] font-semibold">Analysis</h2>
        {status === "ready" ? <span className="truncate text-xs text-game-text2">{`${report?.engine} · depth ${report?.depth}`}</span> : null}
      </div>
      {status === "pending" ? <p role="status" className="flex items-center gap-2 text-sm text-game-text2"><LoaderCircle aria-hidden className="size-4 animate-spin motion-reduce:animate-none" />Analysis pending</p> : null}
      {status === "failed" ? <p role="status" className="text-sm text-game-text2">Analysis unavailable for this game</p> : null}
      {status === "error" ? <div className="flex items-center justify-between gap-2"><p role="alert" className="text-sm">Could not load the analysis.</p><Button variant="outline" className="min-h-11" onClick={retry}>Try again</Button></div> : null}
      {status === "ready" && report ? <>
        <EvalChart positions={report.positions} at={at} onSelect={onSelect} />
        <Verdict positions={report.positions} at={at} san={san} fens={fens} />
      </> : null}
    </section>
  );
}
