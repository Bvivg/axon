"use client";

import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  consequence,
  action,
  pending = false,
  error,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  consequence: string;
  action: string;
  pending?: boolean;
  error?: string | null;
  onConfirm: () => void;
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogTitle>{title}</AlertDialogTitle>
        <AlertDialogDescription>{consequence}</AlertDialogDescription>
        {error ? <Alert className="mt-3">{error}</Alert> : null}
        <div className="mt-5 flex justify-end gap-2">
          <AlertDialogClose render={<Button variant="ghost">Cancel</Button>} />
          <Button variant="destructive" onClick={onConfirm} disabled={pending} aria-busy={pending}>
            {action}
          </Button>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  );
}
