"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef } from "react";

import { useSession } from "@/lib/auth/session";

export const signInPath = "/login";

export const homePath = "/";

export const nextParam = "next";

export function useRequireSession(): void {
  const { status } = useSession();
  const router = useRouter();

  const heldASession = useRef(false);

  useEffect(() => {
    if (status === "authenticated") {
      heldASession.current = true;
      return;
    }
    if (status !== "anonymous") {
      return;
    }

    const here = window.location.pathname + window.location.search;
    const resumable =
      !heldASession.current && here !== "/" && !here.startsWith(signInPath);

    router.replace(
      resumable
        ? `${signInPath}?${nextParam}=${encodeURIComponent(here)}`
        : signInPath,
    );
  }, [status, router]);
}

export function useRedirectWhenSignedIn(): void {
  const { status } = useSession();
  const router = useRouter();

  useEffect(() => {
    if (status !== "authenticated") {
      return;
    }
    router.replace(currentNext());
  }, [status, router]);
}

export function currentNext(): string {
  if (typeof window === "undefined") {
    return homePath;
  }
  return safeNext(new URLSearchParams(window.location.search).get(nextParam));
}

export function safeNext(value: string | null): string {
  if (!value || !value.startsWith("/") || value.startsWith("//")) {
    return homePath;
  }
  return value;
}
