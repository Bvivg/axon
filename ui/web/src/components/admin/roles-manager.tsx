"use client";

import { useState, type FormEvent } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { User } from "@/gen/axon/auth/v1/auth_pb";
import { describe } from "@/lib/errors";
import { useAssignRole, useLookupUser, useRevokeRole, useRoles } from "@/lib/query/admin";

export function RolesManager() {
  const roles = useRoles();
  const lookupUser = useLookupUser();
  const assignRole = useAssignRole();
  const revokeRole = useRevokeRole();

  const [email, setEmail] = useState("");
  const [found, setFound] = useState<User | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function lookup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    try {
      const user = await lookupUser.mutateAsync(email.trim());
      setFound(user);
    } catch (err) {
      setFound(null);
      setError(describe(err));
    }
  }

  async function toggleRole(roleId: string, granted: boolean) {
    if (!found) {
      return;
    }
    setError(null);
    try {
      if (granted) {
        await revokeRole.mutateAsync({ userId: found.id, roleId });
      } else {
        await assignRole.mutateAsync({ userId: found.id, roleId });
      }
      setFound(await lookupUser.mutateAsync(found.email));
    } catch (err) {
      setError(describe(err));
    }
  }

  if (roles.isError) {
    return (
      <div className="mx-auto w-full max-w-2xl px-6 py-12">
        <Alert>{describe(roles.error)}</Alert>
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-2xl px-6 py-12">
      <Card>
        <CardHeader>
          <CardTitle>Roles</CardTitle>
        </CardHeader>
        <CardContent className="space-y-6">
          <form className="flex items-end gap-3" onSubmit={(e) => void lookup(e)}>
            <div className="flex-1 space-y-2">
              <Label htmlFor="lookup-email">Email</Label>
              <Input
                id="lookup-email"
                type="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <Button type="submit" disabled={lookupUser.isPending}>
              {lookupUser.isPending ? "Looking up…" : "Look up"}
            </Button>
          </form>

          {error ? <Alert>{error}</Alert> : null}

          {found ? (
            <div className="space-y-3 rounded-lg border border-border p-4">
              <div>
                <p className="text-sm font-medium">{found.displayName || found.email}</p>
                <p className="text-xs text-muted-foreground">{found.email}</p>
              </div>

              <ul className="divide-y divide-border">
                {(roles.data ?? []).map((role) => {
                  const granted = found.roles.includes(role.name);
                  return (
                    <li key={role.id} className="flex items-center justify-between py-3">
                      <span className="text-sm font-medium">{role.name}</span>
                      <Button
                        type="button"
                        variant={granted ? "outline" : "default"}
                        size="sm"
                        disabled={assignRole.isPending || revokeRole.isPending}
                        onClick={() => void toggleRole(role.id, granted)}
                      >
                        {granted ? "Revoke" : "Assign"}
                      </Button>
                    </li>
                  );
                })}
              </ul>
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
