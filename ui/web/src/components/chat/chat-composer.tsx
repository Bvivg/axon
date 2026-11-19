"use client";

import { ArrowLeft, ArrowRight, Users, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";

import { PersonRow } from "@/components/chat/person-row";
import { ListSearchField } from "@/components/ui/list-search-field";
import { DirectoryList } from "@/components/chat/directory-list";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { useSession } from "@/lib/auth/session";
import { openChat } from "@/lib/chat/address";
import { describe } from "@/lib/errors";
import { initials, profileName } from "@/lib/format";
import { useDebouncedValue } from "@/lib/hooks/use-debounced-value";
import { useCreateGroup } from "@/lib/query/chat";
import { useSearchUsers } from "@/lib/query/people";

enum Step {
  Message = "message",
  Members = "members",
  Details = "details",
}

export function ChatComposer({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const { user } = useSession();
  const [step, setStep] = useState(Step.Message);
  const [query, setQuery] = useState("");
  const [name, setName] = useState("");
  const [selected, setSelected] = useState<PublicProfile[]>([]);
  const create = useCreateGroup();
  const needle = query.trim().replace(/^@/, "");
  const debounced = useDebouncedValue(needle, 350);
  const search = useSearchUsers(debounced, step !== Step.Details);
  const people = debounced === needle ? (search.data ?? []).filter((person) => person.id !== user?.id && person.username) : [];
  const loading = needle !== debounced || search.isPending;
  const chosen = new Set(selected.map((person) => person.id));
  const choosing = step === Step.Members;
  const details = step === Step.Details;

  const changeStep = (next: Step) => {
    setQuery("");
    create.reset();
    setStep(next);
  };

  const back = () => {
    if (step === Step.Message) onDone();
    else changeStep(details ? Step.Members : Step.Message);
  };

  const toggle = (person: PublicProfile) => {
    setSelected((current) => current.some((item) => item.id === person.id)
      ? current.filter((item) => item.id !== person.id)
      : [...current, person]);
  };

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (!name.trim() || selected.length === 0 || create.isPending) return;
    create.mutate({ name: name.trim(), memberIDs: selected.map((person) => person.id) }, {
      onSuccess: (room) => {
        if (room) openChat(router, room.id);
        onDone();
      },
    });
  };

  return (
    <section aria-label="New conversation" className="flex min-h-0 flex-1 flex-col" onKeyDown={(event) => {
      if (event.key === "Escape" && !create.isPending) {
        event.stopPropagation();
        if (query) setQuery("");
        else back();
      }
    }}>
      <header className="flex h-17 shrink-0 items-center gap-3 px-4">
        <Button variant="ghost" size="icon" aria-label="Back" disabled={create.isPending} onClick={back}><ArrowLeft /></Button>
        <h1 className="flex-1 text-lg font-semibold">{step === Step.Message ? "New message" : details ? "Group details" : "New group"}</h1>
        {step !== Step.Message ? <span className="font-mono text-xs text-muted-foreground">{details ? "2" : "1"} / 2</span> : null}
      </header>

      {details ? (
        <form onSubmit={onSubmit} className="flex min-h-0 flex-1 flex-col">
          <ScrollArea className="flex-1">
            <div className="flex flex-col items-center gap-5 px-4 pt-4 pb-6">
              <Avatar alt="" fallback={name.trim() ? initials(name) : ""} className="size-20 bg-signal/10 text-2xl text-signal">
                {!name.trim() ? <Users aria-hidden className="size-8" strokeWidth={1.5} /> : null}
              </Avatar>
              <div className="w-full space-y-2">
                <Label htmlFor="group-name">Group name</Label>
                <Input autoFocus id="group-name" placeholder="Give your group a name" value={name} maxLength={120} disabled={create.isPending} onChange={(event) => setName(event.target.value)} />
              </div>
            </div>
            <h2 className="eyebrow px-4 py-2">Members · {selected.length + 1}</h2>
            <div className="flex items-center gap-3 px-4 py-3 text-sm">
              <Avatar alt="" src={user?.avatarUrls?.small || user?.avatarUrl} fallback={initials(profileName(user ?? undefined))} className="size-11" sizes="44px" />
              <span className="flex-1 font-medium">You</span><span className="text-xs text-muted-foreground">Owner</span>
            </div>
            <ul aria-label="Group members">
              {selected.map((person) => (
                <li key={person.id} className="flex items-center gap-3 px-4 py-3">
                  <Avatar alt="" src={person.avatarUrls?.small || person.avatarUrl} fallback={initials(profileName(person))} className="size-11" sizes="44px" />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">{profileName(person)}</span>
                  <Button variant="ghost" size="icon-sm" aria-label={`Remove ${profileName(person)}`} disabled={create.isPending} onClick={() => toggle(person)}><X /></Button>
                </li>
              ))}
            </ul>
          </ScrollArea>
          <div className="shrink-0 space-y-3 border-t border-border p-4">
            {create.isError ? <Alert>{describe(create.error)}</Alert> : null}
            <Button type="submit" variant="signal" size="lg" className="w-full" disabled={!name.trim() || !selected.length || create.isPending} aria-busy={create.isPending}>
              {create.isPending ? "Creating…" : "Create group"}
            </Button>
          </div>
        </form>
      ) : (
        <>
          {choosing && selected.length > 0 ? (
            <ScrollArea className="max-h-32 shrink-0">
              <ul aria-label="Selected people" className="flex flex-wrap gap-1.5 px-4 pb-3">
                {selected.map((person) => (
                  <li key={person.id}>
                    <button type="button" aria-label={`Remove ${profileName(person)}`} onClick={() => toggle(person)} className="flex max-w-full items-center gap-1.5 rounded-full bg-signal/10 py-1 pr-2 pl-1 text-xs hover:bg-signal/20">
                      <Avatar alt="" src={person.avatarUrls?.small || person.avatarUrl} fallback={initials(profileName(person))} className="size-6" sizes="24px" />
                      <span className="max-w-36 truncate">{profileName(person)}</span><X aria-hidden className="size-3" />
                    </button>
                  </li>
                ))}
              </ul>
            </ScrollArea>
          ) : null}
          <ListSearchField key={step} label="Search people" value={query} onChange={setQuery} focus={0} />
          {step === Step.Message ? (
            <button type="button" onClick={() => changeStep(Step.Members)} className="flex w-full shrink-0 items-center gap-3 px-4 py-3 text-left text-sm font-medium text-signal hover:bg-muted">
              <span className="flex size-11 items-center justify-center rounded-full bg-signal/10"><Users aria-hidden className="size-5" /></span>
              <span className="flex-1">Create group</span><ArrowRight aria-hidden className="size-4" />
            </button>
          ) : null}
          <DirectoryList
            key={needle}
            label="People directory"
            search={search}
            loading={loading}
            empty="No people found."
            sections={[{ label: "Available people", title: "People", rows: people.map((person) => ({
              id: person.id,
              height: 56,
              content: <PersonRow profile={person} selected={choosing ? chosen.has(person.id) : undefined} onPick={(profile) => {
                if (choosing) toggle(profile);
                else { openChat(router, profile.username!); onDone(); }
              }} />,
            })) }]}
          />
          {choosing ? (
            <div className="shrink-0 border-t border-border p-4">
              <Button variant="signal" size="lg" className="w-full" disabled={selected.length === 0} onClick={() => changeStep(Step.Details)}>
                Next{selected.length ? ` · ${selected.length}` : ""}<ArrowRight />
              </Button>
            </div>
          ) : null}
        </>
      )}
    </section>
  );
}
