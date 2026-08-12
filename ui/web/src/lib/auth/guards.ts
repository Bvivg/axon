"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef } from "react";

import { useSession } from "@/lib/auth/session";

/**
 * Where a page sends somebody, and where it sends them back to.
 *
 * Both guards below replace rather than push. A bounce is not somewhere the
 * reader chose to be, so leaving it in the history means Back walks into the
 * page that just redirected — and straight back out of it again.
 *
 * Neither guard acts while the session is still being restored. That state
 * exists precisely so a reload does not look anonymous for the moment it takes
 * the refresh to answer; redirecting on it would sign people out on every
 * refresh of a protected page.
 */

/** signInPath is where an anonymous visitor is sent. */
export const signInPath = "/login";

/** homePath is where a signed-in visitor lands with nowhere else to be — Games. */
export const homePath = "/";

/** The query parameter carrying where somebody was headed before being bounced. */
export const nextParam = "next";

/**
 * useRequireSession sends anonymous visitors to sign in, remembering where they
 * were going.
 *
 * The real enforcement is the gateway's: every procedure behind these pages
 * refuses an unauthenticated call. This only decides what to render, which is
 * all a client-side guard can honestly claim to do.
 *
 * Where they were going is only worth remembering if they never got there.
 * Arriving without a session is an interruption, and resuming it is the point of
 * the whole mechanism; losing one on a page that had it is a sign-out, and
 * putting somebody back where they deliberately left is not a kindness. It also
 * means the sign-out button needs no redirect of its own to race with this one —
 * both would be aiming at the same address.
 */
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

/**
 * useRedirectWhenSignedIn takes somebody who is already signed in off the
 * sign-in and registration pages.
 *
 * It prefers where they were going when they were bounced here — the guard
 * above records it — and falls back to the lobby. Returning to the referring
 * page instead would mean reading document.referrer, which is empty as often as
 * not and is somebody else's origin when it is not.
 */
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

/**
 * currentNext reads the destination out of the address bar.
 *
 * A function rather than a hook, and called when a navigation actually happens
 * rather than during render: there is no query string on the server, so a value
 * read while rendering would differ between the markup served and the markup
 * hydrated.
 */
export function currentNext(): string {
  if (typeof window === "undefined") {
    return homePath;
  }
  return safeNext(new URLSearchParams(window.location.search).get(nextParam));
}

/**
 * safeNext keeps a destination from leaving this origin.
 *
 * The parameter travels in a URL anybody can write, so "next" is attacker input
 * on a page that people are trained to type passwords into. Only a path is
 * accepted: "//evil.example" and "https://evil.example" both look relative
 * enough to slip past a naive check, and both leave the site.
 */
export function safeNext(value: string | null): string {
  if (!value || !value.startsWith("/") || value.startsWith("//")) {
    return homePath;
  }
  return value;
}
