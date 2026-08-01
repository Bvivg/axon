"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

import { useSession } from "@/lib/auth/session";
import { Button } from "@/components/ui/button";

export default function Home() {
  const { status } = useSession();
  const router = useRouter();

  useEffect(() => {
    if (status === "authenticated") {
      router.replace("/profile");
    }
  }, [status, router]);

  return (
    <main className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center gap-8 px-6 text-center">
      <div className="space-y-3">
        <h1 className="text-4xl font-semibold tracking-tight">Axon</h1>
        <p className="text-muted-foreground">
          Games, chat and calls over one gateway.
        </p>
      </div>

      {status === "restoring" ? (
        <p className="text-sm text-muted-foreground">Restoring your session…</p>
      ) : (
        <div className="flex w-full gap-3">
          <Button className="flex-1" onClick={() => router.push("/login")}>
            Sign in
          </Button>
          <Button
            variant="outline"
            className="flex-1"
            onClick={() => router.push("/register")}
          >
            Create account
          </Button>
        </div>
      )}

      <p className="text-xs text-muted-foreground">
        <Link className="underline underline-offset-4" href="/login">
          Trouble? Go straight to sign-in.
        </Link>
      </p>
    </main>
  );
}
