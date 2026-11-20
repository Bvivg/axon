"use client";

import { PhoneIncoming, PhoneMissed, PhoneOutgoing, Video } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useState } from "react";

import { NewCallDialog } from "@/components/calls/new-call-dialog";
import { useCall } from "@/components/calls/call-provider";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { ListColumnHeader } from "@/components/ui/list-column";
import { ListIcon, ListItem, ListLabel } from "@/components/ui/list-item";
import { ListSearchField } from "@/components/ui/list-search-field";
import { ScrollArea } from "@/components/ui/scroll-area";
import { CallMode, CallState } from "@/gen/axon/calls/v1/calls_pb";
import { useSession } from "@/lib/auth/session";
import { fromTimestamp, initials, profileName, shortWhen } from "@/lib/format";
import { useCallHistory } from "@/lib/query/calls";
import { usePublicProfiles } from "@/lib/query/people";
import { cn } from "@/lib/utils";

export function CallHistorySidebar() {
  const { user } = useSession();
  const { view } = useCall();
  const [query, setQuery] = useState("");
  const history = useCallHistory();
  const calls = history.data?.pages.flatMap(page => page.calls) ?? [];
  const peers = usePublicProfiles(calls.flatMap((call) => call.participants.filter((p) => p.userId !== user?.id).map((p) => p.userId)));
  const search = useSearchParams();
  const all = search.get("view") === "all";
  const current = all ? undefined : search.get("call") ?? calls[0]?.id;
  const needle = query.trim().toLowerCase();

  return (
    <>
      <ListColumnHeader
        title="Calls"
        action={<NewCallDialog compact disabled={!view.available || view.pending || (!!view.call && view.call.state !== CallState.ENDED)} />}
      />
      <ListSearchField label="Search call history" value={query} onChange={setQuery} />
      <ScrollArea className="min-h-0 flex-1">
        <nav className="flex flex-col gap-0.5 pb-2" aria-label="Call history">
          {calls.length > 0 ? (
            <ListItem href="/calls?view=all" active={all} leading={<ListIcon icon={PhoneIncoming} />} title="All calls" subtitle="Filter by type and media" className="mb-1" />
          ) : null}
          {calls.length > 0 ? <ListLabel>Recent</ListLabel> : null}
          {history.isPending ? <p role="status" className="px-4 py-2 text-xs text-muted-foreground">Loading calls…</p> : null}
          {history.isSuccess && !calls.length ? <p className="px-4 py-2 text-xs text-muted-foreground">No calls yet</p> : null}
          {history.isError ? (
            <div role="alert" className="px-4 py-2 text-xs">
              <p>Could not load calls.</p>
              <Button variant="ghost" size="sm" className="-ml-2.5 mt-1" onClick={() => void history.refetch()}>Try again</Button>
            </div>
          ) : null}
          {calls.map((call) => {
            const peer = call.participants.find((p) => p.userId !== user?.id);
            const profile = peer ? peers.data?.get(peer.userId) : undefined;
            const name = call.kind === "group" ? "Group call" : profileName(profile) || "Direct call";
            if (needle && !name.toLowerCase().includes(needle)) return null;
            const at = fromTimestamp(call.createdAt);
            const missed = call.endReason === "missed" && call.callerId !== user?.id;
            const outgoing = call.callerId === user?.id;
            const Icon = missed ? PhoneMissed : outgoing ? PhoneOutgoing : PhoneIncoming;
            return (
              <ListItem
                key={call.id}
                href={callHistoryHref(call.id)}
                scroll={false}
                active={call.id === current}
                leading={<Avatar src={profile?.avatarUrls?.small || profile?.avatarUrl} fallback={initials(name)} alt="" sizes="32px" className="size-8 rounded-[10px] text-[11px]" />}
                title={name}
                meta={at ? <time dateTime={at.toISOString()}>{shortWhen(at)}</time> : null}
                subtitle={
                  <>
                    <Icon aria-hidden className={cn("size-3.5 shrink-0", missed && "text-destructive")} />
                    <span className={cn("truncate", missed && "text-destructive")}>{missed ? "Missed" : outgoing ? "Outgoing" : "Incoming"}</span>
                    {call.mode === CallMode.VIDEO ? <Video aria-label="Video" className="size-3.5 shrink-0" /> : null}
                  </>
                }
              />
            );
          })}
          {history.hasNextPage ? <Button variant="ghost" size="sm" className="mx-3 mt-1" disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>Load older calls</Button> : null}
        </nav>
      </ScrollArea>
    </>
  );
}

export function callHistoryHref(id: string) {
  return `/calls?call=${encodeURIComponent(id)}`;
}
