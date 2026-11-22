"use client";

import { useId, useRef, useState, type FormEvent, type ReactNode } from "react";
import { toast } from "sonner";

import { Alert } from "@/components/ui/alert";
import { AlertDialog, AlertDialogClose, AlertDialogContent, AlertDialogDescription, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Field, FieldHint, Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { describe } from "@/lib/errors";

export const reasonMin = 3;
export const reasonMax = 500;

export function validReason(reason: string): boolean {
  const length = [...reason.trim()].length;
  return length >= reasonMin && length <= reasonMax;
}

export function ConfirmAction({
  open,
  onOpenChange,
  title,
  consequence,
  confirmLabel,
  destructive = true,
  quiet = false,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  consequence: ReactNode;
  confirmLabel: string;
  destructive?: boolean;
  quiet?: boolean;
  onConfirm: (reason: string) => Promise<unknown>;
}) {
  const id = useId();
  const cancel = useRef<HTMLButtonElement>(null);
  const field = useRef<HTMLTextAreaElement>(null);
  const [reason, setReason] = useState("");
  const [touched, setTouched] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const valid = validReason(reason);

  const change = (next: boolean) => {
    if (pending) return;
    if (!next) {
      setReason("");
      setTouched(false);
      setError(null);
    }
    onOpenChange(next);
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setTouched(true);
    if (pending) return;
    if (!valid) {
      field.current?.focus();
      return;
    }
    setPending(true);
    setError(null);
    try {
      await onConfirm(reason.trim());
      setPending(false);
      setReason("");
      setTouched(false);
      onOpenChange(false);
      if (!quiet) toast.success(`${confirmLabel} · done`);
    } catch (err) {
      setPending(false);
      setError(err);
    }
  };

  return (
    <AlertDialog open={open} onOpenChange={change}>
      <AlertDialogContent initialFocus={cancel} className="w-[min(calc(100vw-2rem),28rem)]">
        <form onSubmit={(event) => void submit(event)} className="flex flex-col gap-4">
          <div>
            <AlertDialogTitle>{title}</AlertDialogTitle>
            <AlertDialogDescription>{consequence}</AlertDialogDescription>
          </div>
          <Field>
            <Label htmlFor={`${id}-reason`}>Reason</Label>
            <Textarea
              ref={field}
              id={`${id}-reason`}
              name="reason"
              rows={3}
              required
              minLength={reasonMin}
              maxLength={reasonMax}
              value={reason}
              aria-invalid={touched && !valid}
              aria-describedby={`${id}-hint`}
              onChange={(event) => setReason(event.target.value)}
              onBlur={() => setTouched(true)}
            />
            <FieldHint id={`${id}-hint`} className={touched && !valid ? "text-destructive" : undefined}>
              Saved to the audit log · {[...reason.trim()].length}/{reasonMax}
            </FieldHint>
          </Field>
          {error ? <Alert>{describe(error)}</Alert> : null}
          <div className="flex justify-end gap-2">
            <AlertDialogClose ref={cancel} disabled={pending} render={<Button variant="outline" />}>Cancel</AlertDialogClose>
            <Button type="submit" variant={destructive ? "destructive" : "default"} disabled={pending} aria-busy={pending}>
              {pending ? "Working…" : confirmLabel}
            </Button>
          </div>
        </form>
      </AlertDialogContent>
    </AlertDialog>
  );
}
