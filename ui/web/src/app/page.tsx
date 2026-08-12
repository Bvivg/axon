"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

import { homePath, signInPath } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";

/**
 * The root is a signpost, not a page.
 *
 * There is nothing here for somebody who is not signed in — every feature is
 * behind an account — so it sends them to sign in, and sends everyone else to
 * the lobby. Both replace rather than push: a redirect nobody chose has no
 * business in the history, where it would catch Back and bounce it forward
 * again.
 */
export default function Home() {
  const { status } = useSession();
  const router = useRouter();

  useEffect(() => {
    if (status === "restoring") {
      return;
    }
    router.replace(status === "authenticated" ? homePath : signInPath);
  }, [status, router]);

  return (
    <main className="flex min-h-screen items-center justify-center">
      <p className="text-sm text-muted-foreground">
        {status === "restoring" ? "Restoring your session…" : "Taking you through…"}
      </p>
    </main>
  );
}
