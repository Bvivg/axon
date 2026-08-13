"use client";

import type { Timestamp } from "@bufbuild/protobuf/wkt";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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

export function Sessions() {
  const { data: sessions, isLoading, error } = useSessions();
  const revoke = useRevokeSession();

  return (
    <div className="mx-auto w-full max-w-2xl px-6 py-12">
      <Card>
        <CardHeader>
          <CardTitle>Sessions</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : error ? (
            <Alert>{describe(error)}</Alert>
          ) : (
            <>
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
                        <p className="flex items-center gap-2 text-sm font-medium">
                          {describeDevice(session.userAgent)}
                          {session.current ? (
                            <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
                              This device
                            </span>
                          ) : null}
                          <span
                            className={
                              session.online
                                ? "inline-flex items-center gap-1 text-xs font-medium text-green-600 dark:text-green-500"
                                : "inline-flex items-center gap-1 text-xs text-muted-foreground"
                            }
                          >
                            <span
                              className={
                                session.online
                                  ? "size-1.5 rounded-full bg-green-500"
                                  : "size-1.5 rounded-full bg-muted-foreground/50"
                              }
                            />
                            {session.online ? "Online" : "Offline"}
                          </span>
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
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
