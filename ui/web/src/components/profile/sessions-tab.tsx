"use client";

import type { Timestamp } from "@bufbuild/protobuf/wkt";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { describe } from "@/lib/errors";
import { useRevokeSession, useSessions } from "@/lib/query/profile";

function toDate(ts?: Timestamp): Date | null {
  return ts ? new Date(Number(ts.seconds) * 1000) : null;
}

function describeDevice(userAgent: string): string {
  if (!userAgent) {
    return "Unknown device";
  }

  const browser = /Edg\//.test(userAgent)
    ? "Edge"
    : /Chrome\//.test(userAgent)
      ? "Chrome"
      : /Firefox\//.test(userAgent)
        ? "Firefox"
        : /Safari\//.test(userAgent)
          ? "Safari"
          : "Browser";

  const os = /Windows/.test(userAgent)
    ? "Windows"
    : /Mac OS X/.test(userAgent)
      ? "macOS"
      : /Android/.test(userAgent)
        ? "Android"
        : /iPhone|iPad/.test(userAgent)
          ? "iOS"
          : /Linux/.test(userAgent)
            ? "Linux"
            : "";

  return os ? `${browser} on ${os}` : browser;
}

export function SessionsTab() {
  const { data: sessions, isLoading, error } = useSessions();
  const revoke = useRevokeSession();

  if (isLoading) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }

  if (error) {
    return <Alert>{describe(error)}</Alert>;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Sessions</CardTitle>
        <CardDescription>
          Every device currently signed in. Ending a session signs that
          device out.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {revoke.isError ? <Alert>{describe(revoke.error)}</Alert> : null}

        {sessions && sessions.length > 0 ? (
          sessions.map((session) => {
            const started = toDate(session.startedAt);
            const lastUsed = toDate(session.lastUsedAt);

            return (
              <div
                key={session.id}
                className="flex items-center justify-between gap-4 rounded-lg border border-border p-3"
              >
                <div className="space-y-1">
                  <p className="text-sm font-medium">
                    {describeDevice(session.userAgent)}
                    {session.current ? (
                      <span className="ml-2 rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
                        This device
                      </span>
                    ) : null}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {session.ip || "unknown IP"} · signed in{" "}
                    {started ? started.toLocaleString() : "—"}
                    {lastUsed ? ` · last used ${lastUsed.toLocaleString()}` : ""}
                  </p>
                </div>

                {!session.current ? (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={revoke.isPending}
                    onClick={() => revoke.mutate(session.id)}
                  >
                    End session
                  </Button>
                ) : null}
              </div>
            );
          })
        ) : (
          <p className="text-sm text-muted-foreground">No active sessions.</p>
        )}
      </CardContent>
    </Card>
  );
}
