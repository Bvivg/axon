"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Users } from "lucide-react";
import type { ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { Session } from "@/gen/axon/game/v1/game_pb";
import { gameClient } from "@/lib/connect/clients";
import { sessionKey, sessionsKey } from "@/lib/query/games";

export function SessionLoader({ id, children }: { id: string; children: (session: Session) => ReactNode }) {
  const client = useQueryClient();
  const query = useQuery({
    queryKey: sessionKey(id),
    queryFn: async ({ signal }) => {
      const { session } = await gameClient.getSession({ sessionId: id }, { signal });
      if (!session) throw new Error("Game unavailable.");
      return session;
    },
    retry: (count, error) => ![Code.NotFound, Code.InvalidArgument, Code.PermissionDenied].includes(ConnectError.from(error).code) && count < 1,
    staleTime: 10_000,
  });
  const join = useMutation({
    mutationFn: async () => {
      const { session } = await gameClient.joinSession({ sessionId: id });
      if (!session) throw new Error("Game unavailable.");
      return session;
    },
    onSuccess: session => {
      client.setQueryData(sessionKey(id), session);
      void client.invalidateQueries({ queryKey: sessionsKey });
    },
  });
  const unknown = ConnectError.from(query.error).code === Code.NotFound;
  const invalid = ConnectError.from(query.error).code === Code.InvalidArgument;
  const joinCode = ConnectError.from(join.error).code;

  return (
    <ScrollArea className="min-h-0 flex-1 bg-game-workspace" viewportProps={{ className: "[&>div]:min-h-full" }}>
      <main className="flex min-h-full w-full flex-col px-[max(16px,env(safe-area-inset-left))] pt-[max(16px,env(safe-area-inset-top))] pb-[max(16px,env(safe-area-inset-bottom))]">
        {query.data ? children(query.data) : (
          <section className="max-w-md space-y-4 rounded-2xl border border-border bg-card p-5">
            {query.isPending ? <p role="status">Loading game…</p> : unknown ? <>
              <h2 className="flex items-center gap-2 font-medium"><Users className="size-5" /> Game invitation</h2>
              <Button variant="signal" className="min-h-11" disabled={join.isPending} onClick={() => join.mutate()}>
                {join.isPending ? "Joining…" : "Join game"}
              </Button>
              {join.isError ? <p role="alert" className="text-sm text-destructive">
                {joinCode === Code.NotFound ? "This game link is unavailable." : joinCode === Code.FailedPrecondition ? "Both seats are already taken." : "Could not join the game. Try again."}
              </p> : null}
            </> : <>
              <p role="alert">{invalid ? "This game link is invalid." : "Could not load the game."}</p>
              {!invalid ? <Button variant="outline" className="min-h-11" onClick={() => void query.refetch()}>Try again</Button> : null}
            </>}
          </section>
        )}
      </main>
    </ScrollArea>
  );
}

