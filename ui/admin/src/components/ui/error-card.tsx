"use client";

import { CircleAlert } from "lucide-react";

import { Button } from "@/components/ui/button";
import { describe, errorCode } from "@/lib/errors";
import { cn } from "@/lib/utils";

export function ErrorCard({
  title,
  error,
  onRetry,
  retrying = false,
  className,
}: {
  title: string;
  error: unknown;
  onRetry?: () => void;
  retrying?: boolean;
  className?: string;
}) {
  return (
    <div
      role="alert"
      className={cn(
        "flex flex-col gap-2.5 rounded-lg border border-destructive/40 bg-destructive/5 p-3.5",
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <CircleAlert aria-hidden className="size-4 shrink-0 text-destructive" strokeWidth={1.5} />
        <span className="text-sm font-semibold">{title}</span>
      </div>
      <code className="block rounded-md bg-muted px-2.5 py-2 font-mono text-xs leading-[18px] break-words">
        [{errorCode(error)}] {describe(error)}
      </code>
      {onRetry ? (
        <div>
          <Button size="sm" onClick={onRetry} disabled={retrying} aria-busy={retrying}>
            Try again
          </Button>
        </div>
      ) : null}
    </div>
  );
}
