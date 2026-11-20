"use client";

import { Copy, Forward, Trash2, X } from "lucide-react";

import { Button } from "@/components/ui/button";

export function SelectionBar({
  count,
  canDelete,
  onCopy,
  onForward,
  onDelete,
  onCancel,
}: {
  count: number;
  canDelete: boolean;
  onCopy: () => void;
  onForward: () => void;
  onDelete: () => void;
  onCancel: () => void;
}) {
  return (
    <div
      role="toolbar"
      aria-label="Selected messages"
      className="absolute inset-0 z-10 flex items-center gap-2 border-t border-border/70 bg-background px-3"
    >
      <Button variant="ghost" size="icon" aria-label="Cancel selection" onClick={onCancel}>
        <X />
      </Button>
      <p className="min-w-0 flex-1 truncate text-sm font-medium" aria-live="polite">
        {count === 1 ? "1 selected" : `${count} selected`}
      </p>
      <Button variant="outline" size="sm" onClick={onCopy} disabled={count === 0}>
        <Copy />
        <span className="max-sm:sr-only">Copy</span>
      </Button>
      {canDelete ? (
        <Button variant="destructive-outline" size="sm" onClick={onDelete} disabled={count === 0}>
          <Trash2 />
          <span className="max-sm:sr-only">Delete</span>
        </Button>
      ) : null}
      <Button variant="signal" size="sm" onClick={onForward} disabled={count === 0}>
        <Forward />
        <span className="max-sm:sr-only">Forward</span>
      </Button>
    </div>
  );
}
