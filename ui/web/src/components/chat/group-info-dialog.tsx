"use client";

import { LogOut, UserMinus, UserPlus } from "lucide-react";
import { useMemo, useState, type FormEvent } from "react";

import { ConfirmDialog } from "@/components/chat/confirm-dialog";
import { PeoplePicker } from "@/components/chat/people-picker";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { MemberRole, type Member } from "@/gen/axon/chat/v1/chat_pb";
import { membersLabel } from "@/lib/chat/content";
import { describe } from "@/lib/errors";
import { useAddGroupMembers, useRemoveGroupMember, useRenameGroup, useRoom } from "@/lib/query/chat";
import { usePublicProfiles } from "@/lib/query/people";

export function GroupInfoDialog({
  open,
  onOpenChange,
  roomID,
  selfID,
  onLeave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  roomID: string;
  selfID: string;
  onLeave: () => void;
}) {
  const room = useRoom(roomID);
  const members = useMemo(() => room.data?.members ?? [], [room.data]);
  const memberIDs = useMemo(() => members.map((m) => m.userId), [members]);
  const profiles = usePublicProfiles(open ? memberIDs : []);

  const owner = members.find((m) => m.userId === selfID)?.role === MemberRole.OWNER;

  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Member | null>(null);

  const nameOf = (member: Member) =>
    profiles.data?.get(member.userId)?.displayName || member.displayName || member.userId.slice(0, 8);

  const sorted = [...members].sort((a, b) => {
    if (a.role !== b.role) return a.role === MemberRole.OWNER ? -1 : b.role === MemberRole.OWNER ? 1 : 0;
    return nameOf(a).localeCompare(nameOf(b));
  });

  const remove = useRemoveGroupMember();

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setAdding(false);
        onOpenChange(next);
      }}
    >
      <DialogContent className="space-y-4">
        <DialogTitle>Group info</DialogTitle>

        {room.isError ? <Alert>{describe(room.error)}</Alert> : null}

        {owner ? (
          <RenameForm key={room.data?.room?.name} roomID={roomID} name={room.data?.room?.name ?? ""} />
        ) : (
          <p className="text-sm font-medium">{room.data?.room?.name}</p>
        )}

        {adding ? (
          <AddPeople roomID={roomID} selfID={selfID} memberIDs={memberIDs} onDone={() => setAdding(false)} />
        ) : (
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <p className="text-xs font-medium text-muted-foreground">{membersLabel(members.length)}</p>
              {owner ? (
                <Button variant="ghost" size="sm" onClick={() => setAdding(true)}>
                  <UserPlus />
                  Add people
                </Button>
              ) : null}
            </div>
            <ScrollArea className="max-h-72">
              <ul aria-label="Members" className="divide-y divide-border">
                {sorted.map((member) => {
                  const profile = profiles.data?.get(member.userId);
                  const name = nameOf(member);
                  return (
                    <li key={member.userId} className="flex items-center gap-3 px-1 py-2">
                      <Avatar
                        src={profile?.avatarUrls?.small || profile?.avatarUrl}
                        alt=""
                        fallback={name.slice(0, 1).toUpperCase()}
                        className="size-9"
                        sizes="36px"
                      />
                      <span className="min-w-0 flex-1 truncate text-sm font-medium">
                        {name}
                        {member.userId === selfID ? <span className="text-muted-foreground"> (you)</span> : null}
                      </span>
                      {member.role === MemberRole.OWNER ? <Badge variant="signal">Owner</Badge> : null}
                      {owner && member.userId !== selfID ? (
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`Remove ${name}`}
                          onClick={() => setRemoving(member)}
                        >
                          <UserMinus />
                        </Button>
                      ) : null}
                    </li>
                  );
                })}
              </ul>
            </ScrollArea>
          </div>
        )}

        <Button variant="destructive-outline" className="w-full" onClick={onLeave}>
          <LogOut />
          Leave group
        </Button>

        <ConfirmDialog
          open={removing !== null}
          onOpenChange={(next) => {
            if (!next) {
              setRemoving(null);
              remove.reset();
            }
          }}
          title={`Remove ${removing ? nameOf(removing) : ""}?`}
          consequence="They stop receiving messages from this group and lose its history."
          action="Remove"
          pending={remove.isPending}
          error={remove.isError ? describe(remove.error) : null}
          onConfirm={() => {
            if (!removing) return;
            remove.mutate({ roomID, userID: removing.userId }, { onSuccess: () => setRemoving(null) });
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

function RenameForm({ roomID, name }: { roomID: string; name: string }) {
  const [draft, setDraft] = useState(name);
  const rename = useRenameGroup();
  const changed = draft.trim() !== "" && draft.trim() !== name;

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    if (!changed) return;
    rename.mutate({ roomID, name: draft.trim() });
  };

  return (
    <form className="space-y-2" onSubmit={onSubmit}>
      <Label htmlFor="group-info-name">Group name</Label>
      <div className="flex gap-2">
        <Input id="group-info-name" value={draft} maxLength={120} onChange={(event) => setDraft(event.target.value)} />
        <Button type="submit" variant="signal" disabled={!changed || rename.isPending} aria-busy={rename.isPending}>
          Save
        </Button>
      </div>
      {rename.isError ? <Alert>{describe(rename.error)}</Alert> : null}
    </form>
  );
}

function AddPeople({
  roomID,
  selfID,
  memberIDs,
  onDone,
}: {
  roomID: string;
  selfID: string;
  memberIDs: string[];
  onDone: () => void;
}) {
  const [people, setPeople] = useState<PublicProfile[]>([]);
  const add = useAddGroupMembers();
  const exclude = useMemo(() => new Set(memberIDs), [memberIDs]);

  return (
    <div className="space-y-3">
      <PeoplePicker selected={people} onChange={setPeople} exclude={exclude} selfID={selfID} />
      {add.isError ? <Alert>{describe(add.error)}</Alert> : null}
      <div className="flex justify-end gap-2">
        <Button variant="ghost" onClick={onDone}>
          Cancel
        </Button>
        <Button
          variant="signal"
          disabled={people.length === 0 || add.isPending}
          aria-busy={add.isPending}
          onClick={() => add.mutate({ roomID, userIDs: people.map((p) => p.id) }, { onSuccess: onDone })}
        >
          Add {people.length > 0 ? people.length : ""}
        </Button>
      </div>
    </div>
  );
}
