"use client";

import Link from "next/link";

import { Button, buttonVariants } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { cn } from "@/lib/utils";

/**
 * The protected page. It proves the whole chain end to end: the access token
 * the gateway issued opens a procedure the policy marks as authenticated.
 *
 * The guard is client-side because the access token lives in memory — a server
 * component has no way to read it, and putting it somewhere the server could
 * read is the arrangement this design exists to avoid. The real enforcement is
 * on the gateway; this only decides what to render.
 */
export default function ProfilePage() {
  const { status, user, signOut } = useSession();

  useRequireSession();

  if (status !== "authenticated" || !user) {
    return (
      <main className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </main>
    );
  }

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-md flex-col justify-center px-6 py-12">
      <Card>
        <CardHeader>
          <CardTitle>{user.displayName || user.email}</CardTitle>
          <CardDescription>You are signed in.</CardDescription>
        </CardHeader>

        <CardContent className="space-y-6">
          <Link
            className={cn(buttonVariants({ variant: "outline" }), "w-full")}
            href="/chat"
          >
            Go to chat
          </Link>

          <dl className="space-y-3 text-sm">
            <Row label="Email" value={user.email} />
            <Row
              label="Email verified"
              value={user.emailVerified ? "yes" : "not yet"}
            />
            <Row label="User ID" value={user.id} mono />
          </dl>

          {/* No redirect here: losing the session is what the guard above
              watches for, and it sends them to sign in. */}
          <Button variant="outline" className="w-full" onClick={() => void signOut()}>
            Sign out
          </Button>
        </CardContent>
      </Card>
    </main>
  );
}

function Row({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={mono ? "font-mono text-xs break-all" : "break-all"}>
        {value}
      </dd>
    </div>
  );
}
