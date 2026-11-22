"use client";

import type { DescMessage, MessageInitShape } from "@bufbuild/protobuf";
import { createStandardSchemaInit } from "@bufbuild/protovalidate";
import type { AnyFieldApi } from "@tanstack/react-form";
import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useId, useState, type FormEvent, type ReactNode } from "react";

import { reasonMax, reasonMin } from "@/components/admin/confirm-action";
import { Alert, FieldError } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NumberField as Stepper } from "@/components/ui/number-field";
import { Field, FieldHint, Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { describe, fieldErrors, isConflict } from "@/lib/errors";
import { cn } from "@/lib/utils";

export type FieldErrors = Record<string, string>;

function capitalise(text: string): string {
  return text ? text.charAt(0).toUpperCase() + text.slice(1) : text;
}

function issueField(path: ReadonlyArray<PropertyKey | { key: PropertyKey }> | undefined): string | undefined {
  for (const segment of [...(path ?? [])].reverse()) {
    const key = typeof segment === "object" ? segment.key : segment;
    if (typeof key === "string") return key;
  }
  return undefined;
}

export function protoErrors<Desc extends DescMessage>(desc: Desc) {
  const schema = createStandardSchemaInit(desc);
  return (init: MessageInitShape<Desc>, fields: readonly string[]): FieldErrors => {
    const result = schema["~standard"].validate(init);
    if (result instanceof Promise || !result.issues) return {};
    const out: FieldErrors = {};
    for (const issue of result.issues) {
      const name = issueField(issue.path);
      if (name && fields.includes(name)) out[name] ??= capitalise(issue.message);
    }
    return out;
  };
}

export function reasonError(reason: string): string | undefined {
  const length = [...reason.trim()].length;
  if (length < reasonMin) return `Write at least ${reasonMin} characters`;
  if (length > reasonMax) return `Keep it under ${reasonMax} characters`;
  return undefined;
}

export function formValidator<V>(check: (value: V) => FieldErrors) {
  return ({ value }: { value: V }) => {
    const fields = check(value);
    return Object.keys(fields).length ? { fields } : undefined;
  };
}

export interface SubmitFailure {
  error: unknown;
  fields: FieldErrors;
  conflict: boolean;
}

const noFailure: SubmitFailure = { error: null, fields: {}, conflict: false };

export function useSubmitFailure(known: readonly string[]) {
  const [failure, setFailure] = useState<SubmitFailure>(noFailure);
  const fail = useCallback((err: unknown) => {
    const all = fieldErrors(err);
    const fields = Object.fromEntries(Object.entries(all).filter(([name]) => known.includes(name)));
    const conflict = isConflict(err);
    setFailure({ error: conflict || Object.keys(fields).length ? null : err, fields, conflict });
  }, [known]);
  const clear = useCallback(() => setFailure(noFailure), []);
  const clearField = useCallback((name: string) => setFailure((current) => {
    if (!(name in current.fields)) return current;
    const fields = { ...current.fields };
    delete fields[name];
    return { ...current, fields };
  }), []);
  return { ...failure, fail, clear, clearField };
}

export function useUnsavedGuard(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);
}

function shownError(field: AnyFieldApi, server?: string): string | undefined {
  if (server) return server;
  if (!field.state.meta.isTouched) return undefined;
  const first = field.state.meta.errors.find(Boolean);
  return typeof first === "string" ? first : undefined;
}

export interface ControlProps {
  id: string;
  name: string;
  "aria-invalid": boolean;
  "aria-describedby": string;
  onBlur: () => void;
}

export function FormField({
  field,
  label,
  hint,
  server,
  children,
}: {
  field: AnyFieldApi;
  label: string;
  hint?: ReactNode;
  server?: string;
  children: (props: ControlProps) => ReactNode;
}) {
  const id = useId();
  const error = shownError(field, server);
  return (
    <Field>
      <Label htmlFor={id}>{label}</Label>
      {children({ id, name: field.name, "aria-invalid": !!error, "aria-describedby": `${id}-note`, onBlur: field.handleBlur })}
      {error ? <FieldError id={`${id}-note`}>{error}</FieldError> : hint ? <FieldHint id={`${id}-note`}>{hint}</FieldHint> : <span id={`${id}-note`} hidden />}
    </Field>
  );
}

export function TextField({ field, label, hint, server, disabled, onEdit, maxLength }: { field: AnyFieldApi; label: string; hint?: ReactNode; server?: string; disabled?: boolean; onEdit?: () => void; maxLength?: number }) {
  return (
    <FormField field={field} label={label} hint={hint} server={server}>
      {(props) => (
        <Input
          {...props}
          value={field.state.value as string}
          disabled={disabled}
          maxLength={maxLength}
          autoComplete="off"
          onChange={(event) => {
            field.handleChange(event.target.value);
            onEdit?.();
          }}
        />
      )}
    </FormField>
  );
}

export function NumberField({ field, label, hint, server, min, max, onEdit }: { field: AnyFieldApi; label: string; hint?: ReactNode; server?: string; min?: number; max?: number; onEdit?: () => void }) {
  return (
    <FormField field={field} label={label} hint={hint} server={server}>
      {(props) => (
        <Stepper
          id={props.id}
          name={props.name}
          invalid={props["aria-invalid"]}
          describedBy={props["aria-describedby"]}
          onBlur={props.onBlur}
          min={min}
          max={max}
          value={Number(field.state.value)}
          onValueChange={(value) => {
            field.handleChange(value === null ? 0 : Math.trunc(value));
            onEdit?.();
          }}
        />
      )}
    </FormField>
  );
}

export function ReasonField({ field, server, onEdit }: { field: AnyFieldApi; server?: string; onEdit?: () => void }) {
  const value = field.state.value as string;
  return (
    <FormField field={field} label="Reason" server={server} hint={`Saved to the audit log · ${[...value.trim()].length}/${reasonMax}`}>
      {(props) => (
        <Textarea
          {...props}
          rows={3}
          maxLength={reasonMax}
          value={value}
          onChange={(event) => {
            field.handleChange(event.target.value);
            onEdit?.();
          }}
        />
      )}
    </FormField>
  );
}

export interface Difference {
  field: string;
  theirs: string;
  yours: string;
}

export function ConflictNotice({ differences, reloading, onReload, onDiscard }: { differences: Difference[]; reloading: boolean; onReload: () => void; onDiscard: () => void }) {
  return (
    <Alert variant="warning">
      <div className="flex flex-col gap-2">
        <p className="font-medium">Someone else saved this record while you were editing.</p>
        {differences.length ? (
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
            {differences.map((d) => (
              <div key={d.field} className="contents">
                <dt className="font-mono text-muted-foreground">{d.field}</dt>
                <dd className="min-w-0 break-words">theirs: {d.theirs || "—"} · yours: {d.yours || "—"}</dd>
              </div>
            ))}
          </dl>
        ) : (
          <p className="text-xs text-muted-foreground">Reload to compare with the saved version.</p>
        )}
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" onClick={onReload} disabled={reloading} aria-busy={reloading}>
            <RefreshCw aria-hidden /> Keep my values and reload
          </Button>
          <Button size="sm" variant="ghost" onClick={onDiscard}>Use their version</Button>
        </div>
      </div>
    </Alert>
  );
}

export function ResourceForm({
  label,
  onSubmit,
  error,
  notice,
  submitLabel,
  submitting,
  canSubmit = true,
  framed = true,
  sticky = false,
  onCancel,
  children,
}: {
  label: string;
  onSubmit: () => void;
  error?: unknown;
  notice?: ReactNode;
  submitLabel: string;
  submitting: boolean;
  canSubmit?: boolean;
  framed?: boolean;
  sticky?: boolean;
  onCancel?: () => void;
  children: ReactNode;
}) {
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    event.stopPropagation();
    onSubmit();
  };
  return (
    <form aria-label={label} noValidate onSubmit={submit} className={cn("flex flex-col gap-4", framed && "rounded-xl border border-line bg-surface p-4", sticky && !framed && "flex-1")}>
      {notice}
      {children}
      {error ? <Alert>{describe(error)}</Alert> : null}
      <div
        className={cn(
          "flex justify-end gap-2",
          sticky && "sticky bottom-0 z-10 mt-auto border-t border-line py-3",
          sticky && (framed ? "-mx-4 -mb-4 rounded-b-xl bg-surface px-4" : "-mx-5 -mb-5 bg-popover px-5"),
        )}
      >
        {onCancel ? <Button variant="outline" className="pointer-coarse:h-11" onClick={onCancel} disabled={submitting}>Cancel</Button> : null}
        <Button type="submit" className="pointer-coarse:h-11" disabled={submitting || !canSubmit} aria-busy={submitting}>{submitting ? "Saving…" : submitLabel}</Button>
      </div>
    </form>
  );
}
