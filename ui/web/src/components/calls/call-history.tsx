"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { MessageSquare, Phone, PhoneMissed, Video } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useRef, useState } from "react";

import { callHistoryHref } from "@/components/calls/call-history-sidebar";
import { CallButtons } from "@/components/calls/call-buttons";
import { NewCallDialog } from "@/components/calls/new-call-dialog";
import { CallPreferences } from "@/components/calls/call-preferences";
import { useCall } from "@/components/calls/call-provider";
import { Avatar } from "@/components/ui/avatar";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { PaneHeader, PaneSection } from "@/components/ui/pane";
import { ScrollArea } from "@/components/ui/scroll-area";
import { CallMode, CallState, type Call } from "@/gen/axon/calls/v1/calls_pb";
import { useSession } from "@/lib/auth/session";
import { chatHref } from "@/lib/chat/address";
import { calendarDate, formatDuration, fromTimestamp, initials, profileName, shortWhen } from "@/lib/format";
import { useCallHistory } from "@/lib/query/calls";
import { usePublicProfiles } from "@/lib/query/people";
import { cn } from "@/lib/utils";

const filters = ["All", "Missed", "Incoming", "Outgoing", "Audio", "Video"] as const;
type Filter = typeof filters[number];

const outcomes: Record<string, string> = {
  declined: "Declined",
  cancelled: "Cancelled",
  busy: "Busy",
  ended: "Completed",
  setup_timeout: "Failed",
  connection_failed: "Failed",
  reconnect_timeout: "Disconnected",
  endpoint_expired: "Disconnected",
  replaced_by_group: "Continued as a group call",
  left: "Left call",
  removed: "Removed",
  participants_left: "Completed",
  media_unavailable: "Media unavailable",
  cleanup_timeout: "Cleanup timed out",
};

function outcomeOf(call: Call, outgoing: boolean): string {
  if (call.state !== CallState.ENDED) return "In progress";
  if (call.endReason === "missed") return outgoing ? "No answer" : "Missed";
  return outcomes[call.endReason] || "Failed";
}

function durationOf(call: Call): number | undefined {
  return call.startedAt && call.endedAt ? Math.max(0, Number(call.endedAt.seconds - call.startedAt.seconds)) * 1000 : undefined;
}

export function CallHistory() {
  const { user } = useSession();
  const { manager, view } = useCall();
  const history = useCallHistory();
  const allCalls = history.data?.pages.flatMap(page => page.calls) ?? [];
  const [filter, setFilter] = useState<Filter>("All");
  const search = useSearchParams();
  const showAll = search.get("view") === "all";
  const selected = showAll ? undefined : allCalls.find((call) => call.id === search.get("call")) ?? allCalls[0];
  const ids = allCalls.flatMap(call => call.participants.filter(p => p.userId !== user?.id).map(p => p.userId));
  const profiles = usePublicProfiles(ids);
  const canStart = view.available && !view.pending && (!view.call || view.call.state === CallState.ENDED);
  const peerOf = (call: Call) => call.participants.find((person) => person.userId !== user?.id)?.userId;
  const nameOf = (call: Call) => {
    const peer = peerOf(call);
    return call.kind === "group" ? "Group call" : profileName(peer ? profiles.data?.get(peer) : undefined) || "Call";
  };

  if (history.isPending) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <PaneHeader title="Calls" actions={<CallPreferences />} />
        <p role="status" className="p-6 text-sm text-muted-foreground">Loading calls…</p>
      </div>
    );
  }

  if (history.isError && !allCalls.length) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <PaneHeader title="Calls" actions={<CallPreferences />} />
        <div role="alert" className="grid flex-1 place-items-center">
          <EmptyState icon={Phone} title="Call history is unavailable">
            <Button variant="outline" onClick={() => { void history.refetch(); }}>Retry</Button>
          </EmptyState>
        </div>
      </div>
    );
  }

  if (!allCalls.length) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <PaneHeader title="Calls" actions={<CallPreferences />} />
        <div className="grid flex-1 place-items-center">
          <EmptyState icon={Phone} title="No calls yet">
            <NewCallDialog disabled={!canStart} label="Call someone" />
          </EmptyState>
        </div>
      </div>
    );
  }

  if (showAll || !selected) {
    return <AllCalls calls={allCalls} filter={filter} onFilter={setFilter} nameOf={nameOf} peerOf={peerOf} canStart={canStart} onRedial={(call) => manager.redial(call)} />;
  }

  const peer = peerOf(selected);
  const profile = peer ? profiles.data?.get(peer) : undefined;
  const name = nameOf(selected);
  const chat = selected.roomId || profile?.username;
  const related = allCalls.filter((call) => call.kind === selected.kind && (selected.kind === "group" ? call.roomId === selected.roomId : peerOf(call) === peer));

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PaneHeader
        back="/calls"
        backLabel="Back to calls"
        leading={<Avatar src={profile?.avatarUrls?.small || profile?.avatarUrl} alt="" fallback={initials(name)} className="size-9 rounded-[11px]" sizes="36px" />}
        title={name}
        subtitle={profile?.username ? `@${profile.username}` : undefined}
        actions={
          <>
            {peer && selected.kind !== "group" ? <CallButtons target={{ peerUserId: peer }} name={name} /> : null}
            {chat ? <Link href={chatHref(chat)} aria-label={`Open chat with ${name}`} className={buttonVariants({ variant: "ghost", size: "icon" })}><MessageSquare /></Link> : null}
            <CallPreferences />
          </>
        }
      />
      <ScrollArea className="min-h-0 flex-1">
        <main className="mx-auto w-full max-w-[720px] space-y-4 px-4 py-5 sm:px-6">
          <PaneSection id="call-history-related" title={selected.kind === "group" ? "Group calls" : `Calls with ${name}`}>
            <ol className="-mx-2 flex flex-col">
              {related.map((call) => (
                <li key={call.id}>
                  <CallRow call={call} outgoing={call.callerId === user?.id} active={call.id === selected.id} />
                </li>
              ))}
            </ol>
          </PaneSection>
        </main>
      </ScrollArea>
    </div>
  );
}

function CallRow({ call, outgoing, active }: { call: Call; outgoing: boolean; active: boolean }) {
  const date = fromTimestamp(call.createdAt);
  const duration = durationOf(call);
  const missed = call.endReason === "missed" && !outgoing;
  const Icon = missed ? PhoneMissed : call.mode === CallMode.VIDEO ? Video : Phone;
  return (
    <Link
      href={callHistoryHref(call.id)}
      scroll={false}
      aria-current={active ? "page" : undefined}
      className={cn("flex min-h-11 items-center gap-3 rounded-[10px] px-2 py-1.5 text-sm transition-colors", active ? "bg-accent" : "hover:bg-muted")}
    >
      <Icon aria-hidden className={cn("size-4 shrink-0", missed ? "text-destructive" : "text-muted-foreground")} />
      <span className={cn("min-w-0 flex-1 truncate", missed && "text-destructive")}>
        {outgoing ? "Outgoing" : "Incoming"} · {outcomeOf(call, outgoing)}{duration !== undefined ? ` · ${formatDuration(duration)}` : ""}
      </span>
      {date ? <time className="shrink-0 text-xs text-muted-foreground" title={calendarDate(date)} dateTime={date.toISOString()}>{shortWhen(date)}</time> : null}
    </Link>
  );
}

function AllCalls({ calls: allCalls, filter, onFilter, nameOf, peerOf, canStart, onRedial }: {
  calls: Call[];
  filter: Filter;
  onFilter: (filter: Filter) => void;
  nameOf: (call: Call) => string;
  peerOf: (call: Call) => string | undefined;
  canStart: boolean;
  onRedial: (call: Call) => void;
}) {
  const { user } = useSession();
  const history = useCallHistory();
  const ids = allCalls.flatMap(call => call.participants.filter(p => p.userId !== user?.id).map(p => p.userId));
  const profiles = usePublicProfiles(ids);
  const viewport = useRef<HTMLDivElement>(null);
  const calls = allCalls.filter(call => {
    const outgoing = call.callerId === user?.id;
    switch (filter) {
      case "Missed": return !outgoing && call.endReason === "missed";
      case "Incoming": return !outgoing;
      case "Outgoing": return outgoing;
      case "Audio": return call.mode === CallMode.AUDIO;
      case "Video": return call.mode === CallMode.VIDEO;
      default: return true;
    }
  });
  const virtual = useVirtualizer({ count: calls.length, getScrollElement: () => viewport.current, estimateSize: () => 60, overscan: 5 });

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PaneHeader back="/calls" backLabel="Back to calls" title="All calls" actions={<CallPreferences />} />
      <div role="group" aria-label="Filter call history" className="flex shrink-0 flex-wrap gap-1 border-b border-line bg-surface px-3 py-2">
        {filters.map(value => (
          <Button key={value} variant={filter === value ? "secondary" : "ghost"} size="sm" className="rounded-full pointer-coarse:min-h-11" aria-pressed={filter === value} onClick={() => {
            onFilter(value);
            viewport.current?.scrollTo({ top: 0 });
          }}>{value}</Button>
        ))}
      </div>
      <ScrollArea className="min-h-0 flex-1" viewportProps={{ ref: viewport }}>
        {!calls.length ? <EmptyState icon={Phone} title={history.hasNextPage ? "No matches in loaded calls" : "No calls match this filter"} /> : null}
        <div className="relative mx-auto w-full max-w-[960px]" style={{ height: virtual.getTotalSize() }}>
          {virtual.getVirtualItems().map(row => {
            const call = calls[row.index];
            const peer = peerOf(call);
            const profile = peer ? profiles.data?.get(peer) : undefined;
            const name = nameOf(call);
            const address = call.roomId || (call.kind !== "group" ? profile?.username : undefined);
            const outgoing = call.callerId === user?.id;
            const missed = call.endReason === "missed" && !outgoing;
            const duration = durationOf(call);
            const date = fromTimestamp(call.createdAt);
            const Icon = missed ? PhoneMissed : call.mode === CallMode.VIDEO ? Video : Phone;
            return (
              <article key={call.id} data-call-id={call.id} className="absolute top-0 left-0 flex w-full items-center gap-2 px-3 sm:px-4" style={{ transform: `translateY(${row.start}px)`, height: row.size }}>
                <Link href={callHistoryHref(call.id)} scroll={false} className="flex min-w-0 flex-1 items-center gap-2 rounded-[10px] px-2 py-1.5 transition-colors hover:bg-muted">
                  <Avatar src={profile?.avatarUrls?.small || profile?.avatarUrl} fallback={initials(name)} alt="" sizes="32px" className="size-8 rounded-[10px] text-[11px]" />
                  <span className="min-w-0 flex-1">
                    <span className="flex items-baseline justify-between gap-2">
                      <span className="truncate text-xs font-semibold" title={name}>{name}</span>
                      {date ? <time className="shrink-0 text-[10px] text-muted-foreground" title={calendarDate(date)} dateTime={date.toISOString()}>{shortWhen(date)}</time> : null}
                    </span>
                    <span className={cn("flex items-center gap-1 truncate text-[11px]", missed ? "text-destructive" : "text-muted-foreground")}>
                      <Icon aria-hidden className="size-3.5 shrink-0" />
                      <span className="truncate">{outgoing ? "Outgoing" : "Incoming"} · {outcomeOf(call, outgoing)}{duration !== undefined ? ` · ${formatDuration(duration)}` : ""}</span>
                    </span>
                  </span>
                </Link>
                {address ? <Link href={chatHref(address)} aria-label={`Open chat with ${name}`} title="Open chat" className={buttonVariants({ variant: "ghost", size: "icon" })}><MessageSquare /></Link> : null}
                {call.kind !== "group" && call.state === CallState.ENDED && peer ? <Button variant="ghost" size="sm" disabled={!canStart} onClick={() => onRedial(call)}>Call back</Button> : null}
              </article>
            );
          })}
        </div>
        {history.isFetchNextPageError ? <p role="alert" className="p-3 text-destructive">Could not load older calls.</p> : null}
        {history.hasNextPage ? <Button className="m-3" variant="outline" disabled={history.isFetchingNextPage} onClick={() => { void history.fetchNextPage(); }}>{history.isFetchNextPageError ? "Retry older calls" : "Load older calls"}</Button> : null}
      </ScrollArea>
    </div>
  );
}
