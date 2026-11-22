"use client";

import { Monitor, Smartphone } from "lucide-react";
import { useState } from "react";

import { ProfilePage, ProfileSection } from "@/components/profile/profile-page";
import { SignInMethods } from "@/components/profile/sign-in-methods";
import { Alert } from "@/components/ui/alert";
import { AlertDialog, AlertDialogClose, AlertDialogContent, AlertDialogDescription, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ErrorCard } from "@/components/ui/error-card";
import { Skeleton } from "@/components/ui/skeleton";
import { describe } from "@/lib/errors";
import { calendarMoment, fromTimestamp } from "@/lib/format";
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
  return at ? calendarMoment(at) : "—";
}

export function Sessions() {
  const sessions = useSessions();
  const revoke = useRevokeSession();
  const [target, setTarget] = useState<{ id: string; device: string }>();
  const [ended, setEnded] = useState("");

  const count = sessions.data?.length ?? 0;
  const sections = [
    { id: "current-device", title: "Current session", sessions: sessions.data?.filter((session) => session.current) ?? [] },
    { id: "other-devices", title: "Other devices", sessions: sessions.data?.filter((session) => !session.current) ?? [] },
  ];

  return (
    <ProfilePage
      title="Sessions"
      aside={
        sessions.data ? (
          <span className="font-mono text-xs text-muted-foreground">{count} active</span>
        ) : null
      }
    >
      <div className="flex flex-col gap-3">
        <SignInMethods />
        {revoke.isError ? <Alert>{describe(revoke.error)}</Alert> : null}
        <div role="status">{ended ? <p className="rounded-lg border border-success/40 bg-success/10 px-3 py-2 text-sm font-medium">{ended}</p> : null}</div>

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
          sections.map((section) => (
            <ProfileSection key={section.id} id={section.id} label={section.title}>
              {section.sessions.length > 0 ? (
                <ul className="flex flex-col overflow-hidden">
                  {section.sessions.map((session) => {
                    const Icon = isMobile(session.userAgent) ? Smartphone : Monitor;
                    const lastUsed = fromTimestamp(session.lastUsedAt);
                    return (
                      <li
                        key={session.id}
                        className="flex min-h-14 flex-wrap items-center gap-x-3 gap-y-2 border-b border-border py-2 last:border-b-0"
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
                              onClick={() => { revoke.reset(); setEnded(""); setTarget({ id: session.id, device: describeDevice(session.userAgent) }); }}
                            >
                              End session
                            </Button>
                          ) : null}
                        </div>
                      </li>
                    );
                  })}
                </ul>
              ) : <p className="text-sm text-muted-foreground">{section.id === "other-devices" ? "No other active sessions." : "Current session unavailable."}</p>}
            </ProfileSection>
          ))
        )}
      </div>
      <AlertDialog open={!!target} onOpenChange={open => { if (!open && !revoke.isPending) setTarget(undefined); }}>
        <AlertDialogContent>
          <AlertDialogTitle>End this session?</AlertDialogTitle>
          <AlertDialogDescription>{target?.device} will be signed out of Axon.</AlertDialogDescription>
          {revoke.isError ? <Alert className="mt-3">{describe(revoke.error)}</Alert> : null}
          <div className="mt-4 flex justify-end gap-2">
            <AlertDialogClose render={<Button variant="outline" className="min-h-11" disabled={revoke.isPending} />}>Cancel</AlertDialogClose>
            <Button variant="destructive" className="min-h-11" disabled={!target || revoke.isPending} onClick={() => {
              if (!target) return;
              revoke.mutate(target.id, { onSuccess: () => { setEnded(`Session ended on ${target.device}.`); setTarget(undefined); } });
            }}>{revoke.isPending ? "Ending…" : "End session"}</Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>
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
