"use client";

import { Phone } from "lucide-react";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";

export default function CallsPage() {
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
        <Phone className="size-8" />
      </div>

      <Card className="w-full">
        <CardHeader>
          <CardTitle>Calling is coming</CardTitle>
          <CardDescription>
            Last on the roadmap, after games — this tab is reserved for it.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm text-muted-foreground">
          Once it lands, the search button below finds somebody by nickname or
          email to call.
        </CardContent>
      </Card>
    </main>
  );
}
