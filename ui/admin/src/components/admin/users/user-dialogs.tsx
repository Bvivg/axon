"use client";

import { useForm, useStore } from "@tanstack/react-form";
import { useState } from "react";
import { toast } from "sonner";

import { ConflictNotice, FormField, formValidator, protoErrors, ReasonField, reasonError, ResourceForm, TextField, useSubmitFailure, type FieldErrors } from "@/components/admin/resource-form";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { UpdateUserProfileRequestSchema, type AdminRole, type AdminUser } from "@/gen/axon/auth/v1/admin_pb";
import { isConflict } from "@/lib/errors";
import { useSetUserRoles, useUpdateUserProfile } from "@/lib/query/admin";

const profileFields = ["displayName", "reason"] as const;
const profileErrors = protoErrors(UpdateUserProfileRequestSchema);

export function checkProfile(userId: string, etag: string) {
  return (values: { displayName: string; reason: string }): FieldErrors => {
    const errors = profileErrors({ userId, etag, displayName: values.displayName.trim(), reason: "placeholder reason" }, profileFields);
    const reason = reasonError(values.reason);
    if (reason) errors.reason = reason;
    return errors;
  };
}

export function ProfileDialog({ open, onOpenChange, user, onReload }: { open: boolean; onOpenChange: (open: boolean) => void; user: AdminUser; onReload: () => Promise<AdminUser | undefined> }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[min(calc(100vw-2rem),30rem)]">
        <DialogTitle className="mb-3 text-base">Edit display name</DialogTitle>
        {open ? <ProfileForm user={user} onDone={() => onOpenChange(false)} onReload={onReload} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function ProfileForm({ user, onDone, onReload }: { user: AdminUser; onDone: () => void; onReload: () => Promise<AdminUser | undefined> }) {
  const save = useUpdateUserProfile();
  const failure = useSubmitFailure(profileFields);
  const [base, setBase] = useState(user);
  const [theirs, setTheirs] = useState<string>();
  const [reloading, setReloading] = useState(false);
  const userId = user.user?.id ?? "";
  const current = base.user?.displayName ?? "";
  const check = checkProfile(userId, base.etag);
  const form = useForm({
    defaultValues: { displayName: user.user?.displayName ?? "", reason: "" },
    validators: { onChange: formValidator(check), onSubmit: formValidator(check) },
    onSubmit: async ({ value }) => {
      failure.clear();
      try {
        await save.mutateAsync({ userId, displayName: value.displayName.trim(), etag: base.etag, reason: value.reason.trim() });
        toast.success("Display name saved");
        onDone();
      } catch (err) {
        failure.fail(err);
        if (isConflict(err)) setTheirs((await onReload())?.user?.displayName ?? "");
      }
    },
  });
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const name = useStore(form.store, (state) => state.values.displayName);
  const reload = async (discard: boolean) => {
    setReloading(true);
    const fresh = await onReload();
    setReloading(false);
    if (!fresh) return;
    setBase(fresh);
    setTheirs(undefined);
    if (discard) form.setFieldValue("displayName", fresh.user?.displayName ?? "");
    failure.clear();
  };
  return (
    <ResourceForm
      label="Display name"
      onSubmit={() => void form.handleSubmit()}
      error={failure.error}
      submitLabel="Save"
      submitting={submitting}
      canSubmit={name.trim() !== current}
      framed={false}
      onCancel={onDone}
      notice={failure.conflict ? (
        <ConflictNotice
          differences={theirs !== undefined && theirs !== name.trim() ? [{ field: "display name", theirs, yours: name.trim() }] : []}
          reloading={reloading}
          onReload={() => void reload(false)}
          onDiscard={() => void reload(true)}
        />
      ) : null}
    >
      <form.Field name="displayName">
        {(field) => <TextField field={field} label="Display name" maxLength={64} server={failure.fields.displayName} onEdit={() => failure.clearField("displayName")} />}
      </form.Field>
      <form.Field name="reason">
        {(field) => <ReasonField field={field} server={failure.fields.reason} onEdit={() => failure.clearField("reason")} />}
      </form.Field>
    </ResourceForm>
  );
}

export function RolesDialog({ open, onOpenChange, user, roles, lockAdmin }: { open: boolean; onOpenChange: (open: boolean) => void; user: AdminUser; roles?: AdminRole[]; lockAdmin: boolean }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[min(calc(100vw-2rem),30rem)]">
        <DialogTitle className="mb-3 text-base">Edit roles</DialogTitle>
        {open ? (roles ? <RolesForm user={user} roles={roles} lockAdmin={lockAdmin} onDone={() => onOpenChange(false)} /> : <Skeleton className="h-32 w-full" />) : null}
      </DialogContent>
    </Dialog>
  );
}

const roleFields = ["roles", "reason"] as const;

function RolesForm({ user, roles, lockAdmin, onDone }: { user: AdminUser; roles: AdminRole[]; lockAdmin: boolean; onDone: () => void }) {
  const save = useSetUserRoles();
  const failure = useSubmitFailure(roleFields);
  const initial = [...(user.user?.roles ?? [])].sort();
  const check = (values: { roles: string[]; reason: string }): FieldErrors => {
    const reason = reasonError(values.reason);
    return reason ? { reason } : {};
  };
  const form = useForm({
    defaultValues: { roles: initial, reason: "" },
    validators: { onChange: formValidator(check), onSubmit: formValidator(check) },
    onSubmit: async ({ value }) => {
      failure.clear();
      try {
        await save.mutateAsync({ userId: user.user?.id ?? "", roles: value.roles, reason: value.reason.trim() });
        toast.success("Roles saved");
        onDone();
      } catch (err) {
        failure.fail(err);
        if (isConflict(err)) onDone();
      }
    },
  });
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const selected = useStore(form.store, (state) => state.values.roles);
  const changed = selected.length !== initial.length || selected.some((name, i) => name !== initial[i]);
  return (
    <ResourceForm label="Roles" onSubmit={() => void form.handleSubmit()} error={failure.error} submitLabel="Save roles" submitting={submitting} canSubmit={changed} framed={false} onCancel={onDone}>
      <form.Field name="roles">
        {(field) => (
          <FormField field={field} label="Roles" server={failure.fields.roles}>
            {(props) => (
              <div role="group" aria-label="Roles" aria-describedby={props["aria-describedby"]} className="flex flex-col divide-y divide-line rounded-lg border border-line">
                {roles.map((role) => {
                  const checked = field.state.value.includes(role.name);
                  const locked = lockAdmin && role.name === "admin" && checked;
                  return (
                    <label key={role.id} className="flex items-center gap-2.5 px-3 py-2 text-sm">
                      <input
                        type="checkbox"
                        className="size-4 accent-signal"
                        checked={checked}
                        disabled={locked}
                        onChange={(event) => {
                          const next = new Set(field.state.value);
                          if (event.target.checked) next.add(role.name);
                          else next.delete(role.name);
                          field.handleChange([...next].sort());
                        }}
                      />
                      <span className="flex min-w-0 flex-col">
                        <span className="font-medium">{role.title || role.name}</span>
                        <span className="font-mono text-xs text-muted-foreground">{role.name} · {role.permissions.length} permissions</span>
                      </span>
                    </label>
                  );
                })}
              </div>
            )}
          </FormField>
        )}
      </form.Field>
      <form.Field name="reason">
        {(field) => <ReasonField field={field} server={failure.fields.reason} onEdit={() => failure.clearField("reason")} />}
      </form.Field>
    </ResourceForm>
  );
}
