"use client";

import { Monitor, Smartphone } from "lucide-react";

import { ProfilePage } from "@/components/profile/profile-page";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ErrorCard } from "@/components/ui/error-card";
import { Skeleton } from "@/components/ui/skeleton";
import { describe } from "@/lib/errors";
import { fromTimestamp } from "@/lib/format";
import { useRevokeSession, useSessions } from "@/lib/query/profile";

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

function isMobile(userAgent: string): boolean {
  return /Android|iPhone|iPad/.test(userAgent);
}

function formatMoment(at: Date | null): string {
  return at
    ? at.toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })
    : "—";
}

export function Sessions() {
  const sessions = useSessions();
  const revoke = useRevokeSession();

  const count = sessions.data?.length ?? 0;

  return (
    <ProfilePage
      title="Sessions"
      aside={
        sessions.data ? (
          <span className="font-mono text-xs text-muted-foreground">{count} active</span>
        ) : null
      }
    >
      <div className="flex flex-col gap-3 pt-6">
        {revoke.isError ? <Alert>{describe(revoke.error)}</Alert> : null}

        {sessions.isPending ? (
          <SessionsSkeleton />
        ) : sessions.isError ? (
          <ErrorCard
            title="Couldn’t load sessions"
            error={sessions.error}
            onRetry={() => void sessions.refetch()}
            retrying={sessions.isFetching}
          />
        ) : count === 0 ? (
          <p className="text-sm text-muted-foreground">No active sessions.</p>
        ) : (
          <ul className="flex flex-col overflow-hidden rounded-lg border border-border">
            {sessions.data.map((session) => {
              const Icon = isMobile(session.userAgent) ? Smartphone : Monitor;
              const lastUsed = fromTimestamp(session.lastUsedAt);
              return (
                <li
                  key={session.id}
                  className="flex min-h-16 flex-wrap items-center gap-x-3 gap-y-2 border-b border-border px-4 py-3 last:border-b-0"
                >
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                    <Icon aria-hidden className="size-[18px]" strokeWidth={1.5} />
                  </span>
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="text-sm font-medium">{describeDevice(session.userAgent)}</span>
                    <span className="truncate font-mono text-2xs text-muted-foreground">
                      {session.ip || "unknown IP"} · signed in {formatMoment(fromTimestamp(session.startedAt))}
                      {lastUsed ? ` · last used ${formatMoment(lastUsed)}` : ""}
                    </span>
                  </div>
                  <div className="flex items-center gap-2">
                    {session.current ? (
                      <Badge variant="signal" dot>
                        This device
                      </Badge>
                    ) : null}
                    <Badge variant={session.online ? "success" : "outline"} dot>
                      {session.online ? "Online" : "Offline"}
                    </Badge>
                    {!session.current ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={revoke.isPending}
                        onClick={() => revoke.mutate(session.id)}
                      >
                        End session
                      </Button>
                    ) : null}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </ProfilePage>
  );
}

function SessionsSkeleton() {
  return (
    <div aria-busy="true" aria-label="Loading sessions" className="flex flex-col gap-5 rounded-lg border border-border p-4">
      {[48, 36].map((width) => (
        <div key={width} className="flex items-center gap-3">
          <Skeleton className="size-9 rounded-lg" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-3" style={{ width: `${width}%` }} />
            <Skeleton className="h-2.5 w-3/4" />
          </div>
        </div>
      ))}
    </div>
  );
}
