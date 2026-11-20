"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import { CallButtons } from "@/components/calls/call-buttons";
import { DirectoryList } from "@/components/chat/directory-list";
import { PersonRow } from "@/components/chat/person-row";
import { ListSearchField } from "@/components/ui/list-search-field";
import { PaneHeader } from "@/components/ui/pane";
import { useSession } from "@/lib/auth/session";
import { openChat } from "@/lib/chat/address";
import { profileName } from "@/lib/format";
import { useDebouncedValue } from "@/lib/hooks/use-debounced-value";
import { useSearchUsers } from "@/lib/query/people";

export default function ContactsPage() {
  const router = useRouter();
  const { user } = useSession();
  const [query, setQuery] = useState("");
  const needle = query.trim().replace(/^@/, "");
  const debounced = useDebouncedValue(needle, 350);
  const search = useSearchUsers(debounced);
  const loading = debounced !== needle || search.isPending;
  const people = loading ? [] : (search.data ?? []).filter((person) => person.id !== user?.id && person.username);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PaneHeader title="Contacts" back="/chat" backLabel="Back to chats" />
      <div className="mx-auto flex min-h-0 w-full max-w-[720px] flex-1 flex-col pt-3">
        <ListSearchField label="Search people" value={query} onChange={setQuery} />
        <DirectoryList
          key={debounced}
          className="min-h-0 flex-1"
          search={search}
          label="Contacts directory"
          loading={loading}
          empty={needle ? "No people match your search." : "No contacts yet."}
          sections={[{
            title: "People",
            label: "People",
            rows: people.map((profile) => ({
              id: profile.id,
              height: 56,
              content: (
                <div className="flex min-w-0 items-center pr-2">
                  <div className="min-w-0 flex-1"><PersonRow profile={profile} onPick={() => { if (profile.username) openChat(router, profile.username); }} /></div>
                  <CallButtons target={{ peerUserId: profile.id }} name={profileName(profile)} />
                </div>
              ),
            })),
          }]}
        />
      </div>
    </div>
  );
}
