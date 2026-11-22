"use client";

import { CheckboxField } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import type { PermissionInfo } from "@/gen/axon/auth/v1/admin_pb";
import { groupPermissions } from "@/lib/query/roles";

export function PermissionPicker({
  catalog,
  value,
  onChange,
  disabled,
  invalid,
  describedBy,
}: {
  catalog?: PermissionInfo[];
  value: string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
  invalid?: boolean;
  describedBy?: string;
}) {
  if (!catalog) return <Skeleton role="status" aria-label="Loading permissions" className="h-32 w-full" />;
  const selected = new Set(value);
  const toggle = (name: string, on: boolean) => {
    const next = new Set(selected);
    if (on) next.add(name);
    else next.delete(name);
    onChange([...next].sort());
  };
  return (
    <div role="group" aria-label="Permissions" aria-describedby={describedBy} data-invalid={invalid || undefined} className="grid gap-3 rounded-lg data-[invalid]:outline-1 data-[invalid]:outline-destructive sm:grid-cols-2">
      {groupPermissions(catalog).map(([namespace, permissions]) => (
        <fieldset key={namespace} className="flex flex-col rounded-lg border border-line px-3 pt-1.5 pb-2">
          <legend className="px-1 font-mono text-xs text-muted-foreground">{namespace}</legend>
          {permissions.map((permission) => (
            <CheckboxField
              key={permission.name}
              label={permission.action}
              checked={selected.has(permission.name)}
              disabled={disabled}
              onCheckedChange={(on) => toggle(permission.name, on)}
            />
          ))}
        </fieldset>
      ))}
    </div>
  );
}
