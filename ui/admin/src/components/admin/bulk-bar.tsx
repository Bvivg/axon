"use client";

import { toast } from "sonner";

import { groupFailures, runBulk, type BulkResult } from "@/lib/admin/bulk";
import { describe } from "@/lib/errors";

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
