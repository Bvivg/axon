"use client";

import { useQuery } from "@tanstack/react-query";
import { Check, Search } from "lucide-react";
import { useId, useState, type KeyboardEvent } from "react";

import { DateRange } from "@/components/admin/date-range";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { CheckboxField } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { FilterKind, joinRange, splitChoices, splitRange, type FilterDef } from "@/lib/admin/list-query";
import { authAdminClient } from "@/lib/connect/clients";
import { initials, personName } from "@/lib/format";
import { searchablePeopleQuery } from "@/lib/people-search";
import { cn } from "@/lib/utils";

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function onEnter(apply: () => void) {
  return (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter") {
      event.preventDefault();
      apply();
    }
  };
}

function TextEditor({ def, value, onChange, autoFocus = false }: EditorProps) {
  const id = useId();
  const [draft, setDraft] = useState(value);
  const apply = () => onChange(draft.trim());
  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={id} className="text-xs text-muted-foreground">{def.label} contains</label>
      <div className="flex gap-2">
        <Input id={id} value={draft} autoFocus={autoFocus} onChange={(event) => setDraft(event.target.value)} onKeyDown={onEnter(apply)} />
        <Button variant="secondary" className="pointer-coarse:h-11" onClick={apply}>Apply</Button>
      </div>
    </div>
  );
}

function IdEditor({ def, value, onChange, autoFocus = false }: EditorProps) {
  const id = useId();
  const [draft, setDraft] = useState(value);
  const valid = uuidPattern.test(draft.trim());
  const apply = () => { if (valid) onChange(draft.trim().toLowerCase()); };
  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={id} className="text-xs text-muted-foreground">{def.label}</label>
      <div className="flex gap-2">
        <Input id={id} value={draft} autoFocus={autoFocus} spellCheck={false} placeholder="00000000-0000-…" className="font-mono text-xs md:text-xs" aria-invalid={!!draft.trim() && !valid} onChange={(event) => setDraft(event.target.value)} onKeyDown={onEnter(apply)} />
        <Button variant="secondary" className="pointer-coarse:h-11" disabled={!valid} onClick={apply}>Apply</Button>
      </div>
      <p className="h-4 text-xs text-destructive">{draft.trim() && !valid ? "Paste a full ID" : ""}</p>
    </div>
  );
}

function useOptimistic(value: string, onChange: (value: string) => void): [string, (next: string) => void] {
  const [local, setLocal] = useState(value);
  const [seen, setSeen] = useState(value);
  if (value !== seen) {
    setSeen(value);
    setLocal(value);
  }
  return [local, (next: string) => {
    setLocal(next);
    onChange(next);
  }];
}

function ChoiceEditor({ def, value, onChange }: EditorProps) {
  const [current, commit] = useOptimistic(value, onChange);
  const picked = new Set(splitChoices(current));
  const toggle = (choice: string, on: boolean) => {
    const next = new Set(picked);
    if (on) next.add(choice);
    else next.delete(choice);
    commit((def.options ?? []).map((option) => option.value).filter((option) => next.has(option)).join(","));
  };
  return (
    <div role="group" aria-label={def.label} className="flex flex-col">
      {(def.options ?? []).map((option) => (
        <CheckboxField key={option.value} label={option.label} checked={picked.has(option.value)} onCheckedChange={(on) => toggle(option.value, on)} />
      ))}
    </div>
  );
}

function NumberEditor({ def, value, onChange, autoFocus = false }: EditorProps) {
  const [from, to] = splitRange(value);
  const [min, setMin] = useState(from);
  const [max, setMax] = useState(to);
  const minID = useId();
  const maxID = useId();
  const clean = (raw: string) => raw.replace(/[^\d-]/g, "");
  const apply = () => onChange(joinRange(clean(min), clean(max)));
  return (
    <div className="flex flex-col gap-2">
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <label htmlFor={minID} className="text-xs text-muted-foreground">{def.label} from</label>
          <Input id={minID} inputMode="numeric" value={min} autoFocus={autoFocus} onChange={(event) => setMin(clean(event.target.value))} onKeyDown={onEnter(apply)} />
        </div>
        <div className="flex flex-col gap-1">
          <label htmlFor={maxID} className="text-xs text-muted-foreground">to</label>
          <Input id={maxID} inputMode="numeric" value={max} onChange={(event) => setMax(clean(event.target.value))} onKeyDown={onEnter(apply)} />
        </div>
      </div>
      <Button variant="secondary" className="pointer-coarse:h-11" onClick={apply}>Apply</Button>
    </div>
  );
}

function FlagEditor({ def, value, onChange }: EditorProps) {
  const [current, commit] = useOptimistic(value, onChange);
  const options = [{ value: "", label: "Any" }, { value: "true", label: "Yes" }, { value: "false", label: "No" }];
  return (
    <div role="radiogroup" aria-label={def.label} className="grid grid-cols-3 gap-1 rounded-lg bg-muted p-0.5">
      {options.map((option) => (
        <button
          key={option.value || "any"}
          type="button"
          role="radio"
          aria-checked={current === option.value}
          onClick={() => commit(option.value)}
          className="min-h-8 rounded-md text-xs font-medium text-muted-foreground transition-colors hover:text-foreground aria-checked:bg-card aria-checked:text-foreground aria-checked:shadow-raised pointer-coarse:min-h-11"
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

function PersonEditor({ def, value, onChange, autoFocus = false }: EditorProps) {
  const access = useAdminAccess();
  const id = useId();
  const [query, setQuery] = useState("");
  const canSearch = access.can(Permission.UsersRead);
  const searchable = canSearch && searchablePeopleQuery(query) && !uuidPattern.test(query.trim());
  const people = useQuery({
    queryKey: ["admin", "people-filter", query.trim()],
    queryFn: async ({ signal }) => (await authAdminClient.listUsers({ query: query.trim(), pageSize: 6 }, { signal })).users,
    enabled: searchable,
    staleTime: 30_000,
  });
  const exact = uuidPattern.test(query.trim());
  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={id} className="text-xs text-muted-foreground">{canSearch ? `${def.label}: name, username, email or ID` : `${def.label} ID`}</label>
      <div className="relative">
        <Search aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input id={id} value={query} autoFocus={autoFocus} className="pl-8 md:pl-8" onChange={(event) => setQuery(event.target.value)} onKeyDown={onEnter(() => { if (exact) onChange(query.trim()); })} />
      </div>
      <ul aria-label="Matching people" className="flex min-h-[12rem] flex-col">
        {exact ? (
          <li>
            <button type="button" className="flex min-h-10 w-full items-center gap-2 rounded-md px-2 text-left text-sm hover:bg-accent pointer-coarse:min-h-11" onClick={() => onChange(query.trim())}>
              <span className="font-mono text-xs">{query.trim()}</span>
            </button>
          </li>
        ) : null}
        {(people.data ?? []).map((admin) => {
          const user = admin.user;
          if (!user) return null;
          const name = personName(user);
          return (
            <li key={user.id}>
              <button
                type="button"
                className={cn("flex min-h-10 w-full items-center gap-2.5 rounded-md px-2 text-left hover:bg-accent pointer-coarse:min-h-11", value === user.id && "bg-accent")}
                onClick={() => onChange(user.id)}
              >
                <Avatar src={user.avatarUrls?.small || user.avatarUrl} alt="" fallback={initials(name)} sizes="24px" className="size-6 rounded-[8px] text-[10px]" />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="truncate text-sm">{name}</span>
                  <span className="truncate text-xs text-muted-foreground">{user.username ? `@${user.username}` : user.email}</span>
                </span>
                {value === user.id ? <Check aria-hidden className="size-4 text-signal" /> : null}
              </button>
            </li>
          );
        })}
        {searchable && people.data?.length === 0 ? <li className="px-2 py-3 text-sm text-muted-foreground">Nobody matches</li> : null}
      </ul>
    </div>
  );
}

interface EditorProps {
  def: FilterDef;
  value: string;
  onChange: (value: string) => void;
  autoFocus?: boolean;
}

export function FilterEditor({ def, value, onChange, autoFocus = false }: EditorProps) {
  switch (def.kind) {
    case FilterKind.Text:
      return <TextEditor key={value} def={def} value={value} onChange={onChange} autoFocus={autoFocus} />;
    case FilterKind.Choice:
      return <ChoiceEditor def={def} value={value} onChange={onChange} />;
    case FilterKind.Number:
      return <NumberEditor key={value} def={def} value={value} onChange={onChange} autoFocus={autoFocus} />;
    case FilterKind.Date:
      return <DateRange value={value} onChange={onChange} />;
    case FilterKind.Person:
      return <PersonEditor def={def} value={value} onChange={onChange} autoFocus={autoFocus} />;
    case FilterKind.Flag:
      return <FlagEditor def={def} value={value} onChange={onChange} />;
    case FilterKind.Id:
      return <IdEditor key={value} def={def} value={value} onChange={onChange} autoFocus={autoFocus} />;
  }
}
