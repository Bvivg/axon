"use client";

import { ConnectError } from "@connectrpc/connect";
import { useEffect, useState } from "react";

import { ProfilePage } from "@/components/profile/profile-page";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { describe } from "@/lib/errors";
import { currentSubscription, disablePush, enablePush, pushSupported } from "@/lib/push";
import { usePushKey } from "@/lib/query/chat";

enum PushState {
  Checking = "checking",
  Unsupported = "unsupported",
  Blocked = "blocked",
  Off = "off",
  On = "on",
}

export function Notifications() {
  const key = usePushKey();
  const [state, setState] = useState(PushState.Checking);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void stateOf().then((next) => {
      if (!cancelled) setState(next);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const toggle = async () => {
    setBusy(true);
    setError(null);
    try {
      if (state === PushState.On) {
        await disablePush();
      } else {
        await enablePush(key.data ?? "");
      }
    } catch (err) {
      setError(err instanceof ConnectError || !(err instanceof Error) ? describe(err) : err.message);
    } finally {
      setState(await stateOf());
      setBusy(false);
    }
  };

  const unavailable = key.isSuccess && key.data === "";
  const loading = state === PushState.Checking || key.isPending;

  return (
    <ProfilePage title="Notifications">
      <div className="flex flex-col gap-3 pt-6">
        {error ? <Alert>{error}</Alert> : null}
        {state === PushState.Blocked ? (
          <Alert variant="warning">
            Notifications are blocked for this site. Allow them in the browser&apos;s site settings.
          </Alert>
        ) : null}
        <div className="flex min-h-16 flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border border-border px-4 py-3">
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="text-sm font-medium">New messages on this device</span>
            <span className="font-mono text-2xs text-muted-foreground">
              {state === PushState.Unsupported
                ? "This browser can't show notifications"
                : unavailable
                  ? "Not set up on this server"
                  : "When Axon isn't open"}
            </span>
          </div>
          {loading ? (
            <Skeleton className="h-8 w-20 rounded-md" />
          ) : state === PushState.Unsupported || unavailable ? null : (
            <div className="flex items-center gap-2">
              <Badge variant={state === PushState.On ? "success" : "outline"} dot>
                {state === PushState.On ? "On" : "Off"}
              </Badge>
              <Button
                variant={state === PushState.On ? "ghost" : "default"}
                size="sm"
                disabled={busy || state === PushState.Blocked}
                onClick={() => void toggle()}
              >
                {state === PushState.On ? "Turn off" : "Turn on"}
              </Button>
            </div>
          )}
        </div>
      </div>
    </ProfilePage>
  );
}

async function stateOf(): Promise<PushState> {
  if (!pushSupported()) return PushState.Unsupported;
  if (Notification.permission === "denied") return PushState.Blocked;
  return (await currentSubscription()) ? PushState.On : PushState.Off;
}
