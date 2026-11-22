"use client";

import { X } from "lucide-react";
import type { ReactNode } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { groupFailures, runBulk, type BulkResult } from "@/lib/admin/bulk";
import { describe } from "@/lib/errors";

export function BulkBar({ count, limit, onClear, children }: { count: number; limit: number; onClear: () => void; children: ReactNode }) {
  if (count === 0) return null;
  return (
    <div role="region" aria-label="Bulk actions" className="flex shrink-0 flex-wrap items-center gap-2 border-b border-line bg-signal/8 px-4 py-2 text-sm">
      <span className="font-medium tabular-nums">{count} selected</span>
      {count >= limit ? <span className="text-xs text-muted-foreground">limit {limit} per action</span> : null}
      <span className="flex flex-1 flex-wrap gap-1.5">{children}</span>
      <Button variant="ghost" size="sm" onClick={onClear}><X aria-hidden /> Clear</Button>
    </div>
  );
}

export async function bulkApply<T>(items: readonly T[], verb: string, run: (item: T) => Promise<unknown>): Promise<BulkResult<T>> {
  const result = await runBulk(items, run);
  if (result.failed.length === items.length && items.length > 0) {
    throw result.failed[0].error;
  }
  if (result.failed.length) {
    const reasons = groupFailures(result.failed, describe).map(([message, n]) => `${n}× ${message}`).join("; ");
    toast.warning(`${verb} ${result.done.length} of ${items.length}. Failed: ${reasons}`);
  } else {
    toast.success(`${verb} ${result.done.length} of ${items.length}`);
  }
  return result;
}
