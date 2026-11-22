"use client";

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
        <fieldset key={namespace} className="flex flex-col gap-1.5 rounded-lg border border-line p-3">
          <legend className="px-1 font-mono text-xs text-muted-foreground">{namespace}</legend>
          {permissions.map((permission) => (
            <label key={permission.name} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="size-4 accent-signal"
                checked={selected.has(permission.name)}
                disabled={disabled}
                onChange={(event) => toggle(permission.name, event.target.checked)}
              />
              <span>{permission.action}</span>
            </label>
          ))}
        </fieldset>
      ))}
    </div>
  );
}
