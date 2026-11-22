"use client";

import { useForm, useStore } from "@tanstack/react-form";
import { useState } from "react";
import { toast } from "sonner";

import { ConflictNotice, formValidator, NumberField, protoErrors, ReasonField, reasonError, ResourceForm, TextField, useSubmitFailure, useUnsavedGuard, type Difference, type FieldErrors } from "@/components/admin/resource-form";
import { UpdateTournamentRequestSchema } from "@/gen/axon/tournament/v1/admin_pb";
import { TournamentFormat, type Tournament } from "@/gen/axon/tournament/v1/tournament_pb";
import { isConflict } from "@/lib/errors";
import { useAdminTournament, useUpdateTournament } from "@/lib/query/play";

export interface TournamentValues {
  name: string;
  capacity: number;
  rounds: number;
  reason: string;
}

const known = ["name", "capacity", "rounds", "reason"] as const;
const requestErrors = protoErrors(UpdateTournamentRequestSchema);

export function bracketSizes(): number[] {
  return [4, 8, 16];
}

export function checkTournament(base: Tournament) {
  const swiss = base.format === TournamentFormat.SWISS;
  return (values: TournamentValues): FieldErrors => {
    const errors = requestErrors({ tournamentId: base.id, name: values.name.trim(), capacity: values.capacity, rounds: values.rounds, updateMask: { paths: ["name"] }, etag: base.etag || "W/\"1\"", reason: "placeholder" }, known);
    if (!values.name.trim()) errors.name ??= "Name the tournament";
    if (values.capacity < base.playerCount) errors.capacity ??= `${base.playerCount} players are already registered`;
    if (!swiss && !bracketSizes().includes(values.capacity)) errors.capacity ??= "A bracket holds 4, 8 or 16 players";
    if (swiss && (values.rounds < 1 || values.rounds >= values.capacity)) errors.rounds ??= "Play at least one round and fewer rounds than players";
    const reason = reasonError(values.reason);
    if (reason) errors.reason = reason;
    return errors;
  };
}

export function changedTournamentPaths(base: Tournament, values: TournamentValues): string[] {
  const paths: string[] = [];
  if (values.name.trim() !== base.name) paths.push("name");
  if (values.capacity !== base.capacity) paths.push("capacity");
  if (base.format === TournamentFormat.SWISS && values.rounds !== base.totalRounds) paths.push("rounds");
  return paths;
}

function differences(theirs: Tournament, values: TournamentValues): Difference[] {
  const out: Difference[] = [];
  if (theirs.name !== values.name.trim()) out.push({ field: "name", theirs: theirs.name, yours: values.name.trim() });
  if (theirs.capacity !== values.capacity) out.push({ field: "capacity", theirs: String(theirs.capacity), yours: String(values.capacity) });
  if (theirs.format === TournamentFormat.SWISS && theirs.totalRounds !== values.rounds) out.push({ field: "rounds", theirs: String(theirs.totalRounds), yours: String(values.rounds) });
  return out;
}

function valuesOf(t: Tournament): TournamentValues {
  return { name: t.name, capacity: t.capacity, rounds: t.totalRounds, reason: "" };
}

export function TournamentForm({ tournament, onDone }: { tournament: Tournament; onDone: () => void }) {
  const save = useUpdateTournament();
  const remote = useAdminTournament(tournament.id);
  const [base, setBase] = useState(tournament);
  const failure = useSubmitFailure(known);
  const swiss = base.format === TournamentFormat.SWISS;
  const check = checkTournament(base);

  const form = useForm({
    defaultValues: valuesOf(tournament),
    validators: { onChange: formValidator(check), onSubmit: formValidator(check) },
    onSubmit: async ({ value }) => {
      failure.clear();
      try {
        await save.mutateAsync({
          tournamentId: base.id,
          name: value.name.trim(),
          capacity: value.capacity,
          rounds: value.rounds,
          paths: changedTournamentPaths(base, value),
          etag: base.etag,
          reason: value.reason.trim(),
        });
        toast.success("Tournament saved");
        onDone();
      } catch (err) {
        failure.fail(err);
        if (isConflict(err)) void remote.refetch();
      }
    },
  });

  const values = useStore(form.store, (state) => state.values);
  const dirty = useStore(form.store, (state) => state.isDirty);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  useUnsavedGuard(dirty && !submitting);
  const paths = changedTournamentPaths(base, values);
  const theirs = failure.conflict && remote.data && remote.data.etag !== base.etag ? remote.data : undefined;

  const reload = async (discard: boolean) => {
    const fresh = (await remote.refetch()).data;
    if (!fresh) return;
    setBase(fresh);
    if (discard) form.reset({ ...valuesOf(fresh), reason: values.reason });
    failure.clear();
  };

  return (
    <ResourceForm
      label={`Edit ${base.name}`}
      onSubmit={() => void form.handleSubmit()}
      error={failure.error}
      submitLabel="Save changes"
      submitting={submitting}
      canSubmit={paths.length > 0}
      onCancel={onDone}
      notice={failure.conflict ? (
        <ConflictNotice differences={theirs ? differences(theirs, values) : []} reloading={remote.isFetching} onReload={() => void reload(false)} onDiscard={() => void reload(true)} />
      ) : null}
    >
      <form.Field name="name">
        {(field) => <TextField field={field} label="Name" maxLength={64} server={failure.fields.name} onEdit={() => failure.clearField("name")} />}
      </form.Field>
      <div className="flex flex-wrap gap-4">
        <form.Field name="capacity">
          {(field) => (
            <NumberField
              field={field}
              label="Places"
              min={swiss ? 3 : 4}
              max={swiss ? 64 : 16}
              server={failure.fields.capacity}
              onEdit={() => failure.clearField("capacity")}
              hint={swiss ? "3 to 64 players" : "4, 8 or 16 players"}
            />
          )}
        </form.Field>
        {swiss ? (
          <form.Field name="rounds">
            {(field) => <NumberField field={field} label="Rounds" min={1} max={9} server={failure.fields.rounds} onEdit={() => failure.clearField("rounds")} hint="1 to 9, fewer than players" />}
          </form.Field>
        ) : null}
      </div>
      <form.Field name="reason">
        {(field) => <ReasonField field={field} server={failure.fields.reason} onEdit={() => failure.clearField("reason")} />}
      </form.Field>
    </ResourceForm>
  );
}
