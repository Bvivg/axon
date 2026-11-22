"use client";

import { useEffect } from "react";

import { SessionStatus, useSession } from "@/lib/auth/session";
import { appHomeHref, appSignInHref } from "@/lib/base-path";

export function leaveToSignIn(): void {
  const here = window.location.pathname + window.location.search;
  window.location.assign(appSignInHref(here));
}

export function leaveToApp(): void {
  window.location.assign(appHomeHref);
}

export function useRequireSession(): void {
  const { status } = useSession();
  useEffect(() => {
    if (status === SessionStatus.Anonymous) leaveToSignIn();
  }, [status]);
}
