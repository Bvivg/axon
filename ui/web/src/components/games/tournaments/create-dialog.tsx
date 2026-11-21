"use client";

import { Plus } from "lucide-react";
import { useRouter } from "next/navigation";
import { useRef, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Field, Label } from "@/components/ui/label";
import { TournamentFormat } from "@/gen/axon/tournament/v1/tournament_pb";
import { GameKey, games, tournamentGameKeys } from "@/lib/games/catalog";
import { tournamentFormatLabel, tournamentHref, useCreateTournament } from "@/lib/query/tournaments";
import { cn } from "@/lib/utils";

const capacities = [4, 8, 16];
const swissRounds = [3, 4, 5];
const formats = [TournamentFormat.SINGLE_ELIMINATION, TournamentFormat.SWISS];

function Choice<T extends string | number>({ label, options, value, onChange, render }: {
  label: string;
  options: T[];
  value: T;
  onChange: (next: T) => void;
  render: (option: T) => ReactNode;
}) {
  return (
    <Field>
      <span className="text-base font-medium text-foreground md:text-sm">{label}</span>
      <div role="group" aria-label={label} className="flex flex-wrap gap-1.5">
        {options.map(option => (
          <button
            key={option}
            type="button"
            aria-pressed={option === value}
            onClick={() => onChange(option)}
            className={cn(
              "min-h-9 rounded-[10px] border px-3 text-sm transition-colors",
              option === value ? "border-signal bg-signal/15 text-signal" : "border-line bg-surface hover:bg-accent",
            )}
          >
            {render(option)}
          </button>
        ))}
      </div>
    </Field>
  );
}

export function CreateTournamentDialog() {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [game, setGame] = useState<GameKey>(GameKey.TicTacToe);
  const [format, setFormat] = useState(TournamentFormat.SINGLE_ELIMINATION);
  const [capacity, setCapacity] = useState(8);
  const [rounds, setRounds] = useState(3);
  const requestID = useRef<string | undefined>(undefined);
  const create = useCreateTournament();
  const trimmed = name.trim();

  const submit = () => {
    requestID.current ??= crypto.randomUUID();
    create.mutate(
      { requestId: requestID.current, name: trimmed, game, format, capacity, rounds: format === TournamentFormat.SWISS ? rounds : undefined },
      {
        onSuccess: tournament => {
          requestID.current = undefined;
          setOpen(false);
          setName("");
          router.push(tournamentHref(tournament.id));
        },
      },
    );
  };

  return (
    <>
      <Button variant="signal" size="sm" className="gap-1.5" onClick={() => setOpen(true)}>
        <Plus aria-hidden />New tournament
      </Button>
      <Dialog open={open} onOpenChange={next => { if (!create.isPending) setOpen(next); }}>
        <DialogContent className="space-y-4">
          <DialogTitle>New tournament</DialogTitle>
          <form
            className="space-y-4"
            onSubmit={event => {
              event.preventDefault();
              if (trimmed) submit();
            }}
          >
            <Field>
              <Label htmlFor="tournament-name">Name</Label>
              <Input id="tournament-name" value={name} maxLength={64} autoComplete="off" onChange={event => setName(event.target.value)} />
            </Field>
            <Choice label="Game" options={tournamentGameKeys} value={game} onChange={setGame} render={key => games[key].title} />
            <Choice label="Format" options={formats} value={format} onChange={setFormat} render={tournamentFormatLabel} />
            <Choice label="Players" options={capacities} value={capacity} onChange={setCapacity} render={String} />
            {format === TournamentFormat.SWISS ? (
              <Choice label="Rounds" options={swissRounds} value={rounds} onChange={setRounds} render={String} />
            ) : null}
            {create.isError ? <p role="alert" className="text-sm text-destructive">Could not create the tournament. Try again.</p> : null}
            <div className="flex justify-end gap-2">
              <Button variant="outline" disabled={create.isPending} onClick={() => setOpen(false)}>Cancel</Button>
              <Button type="submit" variant="signal" disabled={!trimmed || create.isPending} aria-busy={create.isPending}>
                {create.isPending ? "Creating…" : "Create"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
