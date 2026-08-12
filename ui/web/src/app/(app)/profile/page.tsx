"use client";

import { ProfileTab } from "@/components/profile/profile-tab";
import { SessionsTab } from "@/components/profile/sessions-tab";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";

export default function ProfilePage() {
  const { status, user } = useSession();

  useRequireSession();

  if (status !== "authenticated" || !user) {
    return (
      <main className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </main>
    );
  }

  return (
    <main className="mx-auto w-full max-w-2xl px-6 py-12">
      <Tabs defaultValue="profile" className="space-y-6">
        <TabsList>
          <TabsTrigger value="profile">My profile</TabsTrigger>
          <TabsTrigger value="sessions">Sessions</TabsTrigger>
        </TabsList>

        <TabsContent value="profile">
          <ProfileTab user={user} />
        </TabsContent>

        <TabsContent value="sessions">
          <SessionsTab />
        </TabsContent>
      </Tabs>
    </main>
  );
}
