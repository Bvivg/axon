"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef } from "react";

import { useSession } from "@/lib/auth/session";
import { HistoryMode, navigateTo } from "@/lib/chat/address";

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

    const here = window.location.pathname + window.location.search + window.location.hash;
    const resumable =
      !heldASession.current && here !== "/" && !here.startsWith(signInPath);

    router.replace(
      resumable
        ? `${signInPath}?${nextParam}=${encodeURIComponent(here)}`
        : signInPath,
    );
  }, [status, router]);
}

export function useRequireRole(role: string): boolean {
  const { status, user } = useSession();
  const router = useRouter();

  useRequireSession();

  const authorized = status === "authenticated" && !!user?.roles.includes(role);

  useEffect(() => {
    if (status === "authenticated" && !authorized) {
      router.replace(homePath);
    }
  }, [status, authorized, router]);

  return authorized;
}

export function useRedirectWhenSignedIn(): void {
  const { status } = useSession();
  const router = useRouter();

  useEffect(() => {
    if (status !== "authenticated") {
      return;
    }
    navigateTo(router, currentNext(), HistoryMode.Replace);
  }, [status, router]);
}

export function currentNext(): string {
  if (typeof window === "undefined") {
    return homePath;
  }
  return safeNext(new URLSearchParams(window.location.search).get(nextParam));
}

export function safeNext(value: string | null): string {
  if (!value || !value.startsWith("/")) {
    return homePath;
  }
  const target = new URL(value, window.location.origin);
  if (target.origin !== window.location.origin) {
    return homePath;
  }
  return target.pathname + target.search + target.hash;
}
