"use client";

import { Gamepad2 } from "lucide-react";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";

export default function GamesHomePage() {
  const { status } = useSession();

  useRequireSession();

  if (status !== "authenticated") {
    return (
      <main className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </main>
    );
  }

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-2xl flex-col items-center justify-center gap-6 px-6 py-12 text-center">
      <div className="flex size-16 items-center justify-center rounded-full bg-primary/10 text-primary">
        <Gamepad2 className="size-8" />
      </div>

      <Card className="w-full">
        <CardHeader>
          <CardTitle>Games are coming</CardTitle>
          <CardDescription>
            The board and the matchmaking aren&apos;t here yet — this tab is
            reserved for them.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          When a game is open, joining one by its code happens from the search
          button below.
        </CardContent>
      </Card>
    </main>
  );
}
