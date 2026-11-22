"use client";

import { useForm, useStore } from "@tanstack/react-form";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";

import { ConflictNotice, FormField, formValidator, protoErrors, ReasonField, reasonError, ResourceForm, TextField, useSubmitFailure, useUnsavedGuard, type Difference, type FieldErrors } from "@/components/admin/resource-form";
import { PermissionPicker } from "@/components/admin/roles/permission-picker";
import { roleHref } from "@/components/admin/roles/roles-list";
import { AdminRoleSchema, type AdminRole } from "@/gen/axon/auth/v1/admin_pb";
import { isConflict } from "@/lib/errors";
import { useAdminRole, useCreateRole, usePermissionCatalog, useUpdateRole } from "@/lib/query/roles";

export interface RoleValues {
  name: string;
  title: string;
  permissions: string[];
  reason: string;
}

const known = ["name", "title", "permissions", "reason"] as const;
const roleErrors = protoErrors(AdminRoleSchema);

const nameRule = "Use 2 to 32 lowercase letters, digits or underscores, starting with a letter";

export function friendlyRoleError(field: string, message: string | undefined): string | undefined {
  return field === "name" && message && /regex/i.test(message) ? nameRule : message;
}

export function checkRole(values: RoleValues): FieldErrors {
  const errors = roleErrors({ name: values.name.trim(), title: values.title.trim(), permissions: values.permissions }, known);
  if (errors.name) errors.name = friendlyRoleError("name", errors.name) ?? errors.name;
  const reason = reasonError(values.reason);
  if (reason) errors.reason = reason;
  return errors;
}

function samePermissions(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && [...a].sort().every((name, i) => name === [...b].sort()[i]);
}

export function changedRolePaths(base: AdminRole, values: RoleValues): string[] {
  const paths: string[] = [];
  if (values.title.trim() !== base.title) paths.push("title");
  if (!samePermissions(values.permissions, base.permissions)) paths.push("permissions");
  return paths;
}

export function roleDifferences(theirs: AdminRole, values: RoleValues): Difference[] {
  const out: Difference[] = [];
  if (theirs.title !== values.title.trim()) out.push({ field: "title", theirs: theirs.title, yours: values.title.trim() });
  if (!samePermissions(theirs.permissions, values.permissions)) {
    out.push({ field: "permissions", theirs: [...theirs.permissions].sort().join(", "), yours: [...values.permissions].sort().join(", ") });
  }
  return out;
}

function valuesOf(role?: AdminRole): RoleValues {
  return { name: role?.name ?? "", title: role?.title ?? "", permissions: [...(role?.permissions ?? [])].sort(), reason: "" };
}

export function RoleForm({ role }: { role?: AdminRole }) {
  const router = useRouter();
  const catalog = usePermissionCatalog();
  const create = useCreateRole();
  const update = useUpdateRole();
  const remote = useAdminRole(role?.id ?? "");
  const [base, setBase] = useState(role);
  const [requestId] = useState(() => crypto.randomUUID());
  const failure = useSubmitFailure(known);

  const form = useForm({
    defaultValues: valuesOf(role),
    validators: { onChange: formValidator(checkRole), onSubmit: formValidator(checkRole) },
    onSubmit: async ({ value }) => {
      failure.clear();
      const reason = value.reason.trim();
      try {
        if (!base) {
          const created = await create.mutateAsync({ role: { name: value.name.trim(), title: value.title.trim(), permissions: value.permissions }, reason, requestId });
          toast.success(`Role ${created.name} created`);
          router.push(roleHref(created.id));
          return;
        }
        const updated = await update.mutateAsync({
          role: { id: base.id, name: base.name, title: value.title.trim(), permissions: value.permissions, etag: base.etag },
          paths: changedRolePaths(base, value),
          reason,
        });
        toast.success(`Role ${updated.name} saved`);
        router.push(roleHref(updated.id));
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

  const paths = base ? changedRolePaths(base, values) : [];
  const theirs = failure.conflict && remote.data && remote.data.etag !== base?.etag ? remote.data : undefined;

  const keepMine = async () => {
    const fresh = (await remote.refetch()).data;
    if (!fresh) return;
    setBase(fresh);
    failure.clear();
  };

  const takeTheirs = async () => {
    const fresh = (await remote.refetch()).data;
    if (!fresh) return;
    setBase(fresh);
    form.reset({ ...valuesOf(fresh), reason: values.reason });
    failure.clear();
  };

  return (
    <ResourceForm
      label={base ? `Edit role ${base.name}` : "New role"}
      onSubmit={() => void form.handleSubmit()}
      error={failure.error}
      submitLabel={base ? "Save changes" : "Create role"}
      submitting={submitting}
      canSubmit={!base || paths.length > 0}
      onCancel={() => router.push(base ? roleHref(base.id) : "/roles")}
      notice={failure.conflict ? (
        <ConflictNotice
          differences={theirs ? roleDifferences(theirs, values) : []}
          reloading={remote.isFetching}
          onReload={() => void keepMine()}
          onDiscard={() => void takeTheirs()}
        />
      ) : null}
    >
      <form.Field name="name">
        {(field) => (
          <TextField
            field={field}
            label="Name"
            maxLength={32}
            disabled={!!base}
            server={friendlyRoleError("name", failure.fields.name)}
            onEdit={() => failure.clearField("name")}
            hint={base ? "The name is part of every token and cannot change" : nameRule}
          />
        )}
      </form.Field>
      <form.Field name="title">
        {(field) => <TextField field={field} label="Title" maxLength={64} server={failure.fields.title} onEdit={() => failure.clearField("title")} />}
      </form.Field>
      <form.Field name="permissions">
        {(field) => (
          <FormField field={field} label="Permissions" server={failure.fields.permissions} hint={`${field.state.value.length} selected`}>
            {(props) => (
              <PermissionPicker
                catalog={catalog.data}
                value={field.state.value}
                invalid={props["aria-invalid"]}
                describedBy={props["aria-describedby"]}
                onChange={(next) => {
                  field.handleChange(next);
                  field.handleBlur();
                  failure.clearField("permissions");
                }}
              />
            )}
          </FormField>
        )}
      </form.Field>
      <form.Field name="reason">
        {(field) => <ReasonField field={field} server={failure.fields.reason} onEdit={() => failure.clearField("reason")} />}
      </form.Field>
      {base && paths.length === 0 ? <p className="text-xs text-muted-foreground">Change the title or permissions to save.</p> : null}
    </ResourceForm>
  );
}
