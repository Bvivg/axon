"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";
import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { joinRange, splitRange } from "@/lib/admin/list-query";
import { appLocale } from "@/lib/format";
import { cn } from "@/lib/utils";

function iso(date: Date): string {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

function fromIso(value: string): Date | null {
  const [y, m, d] = value.split("-").map(Number);
  if (!y || !m || !d) return null;
  return new Date(y, m - 1, d);
}

function addDays(date: Date, days: number): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + days);
}

const weekdays = Array.from({ length: 7 }, (_, i) => new Date(2024, 0, 1 + i).toLocaleDateString(appLocale, { weekday: "narrow" }));

function monthGrid(month: Date): Date[] {
  const first = new Date(month.getFullYear(), month.getMonth(), 1);
  const lead = (first.getDay() + 6) % 7;
  return Array.from({ length: 42 }, (_, i) => addDays(first, i - lead));
}

export function DateRange({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const [from, to] = splitRange(value);
  const today = useMemo(() => new Date(), []);
  const [month, setMonth] = useState(() => {
    const anchor = fromIso(to || from) ?? today;
    return new Date(anchor.getFullYear(), anchor.getMonth(), 1);
  });
  const [draft, setDraft] = useState<string>("");
  const start = draft || from;
  const end = draft ? "" : to;
  const presets = [
    { label: "Today", range: joinRange(iso(today), iso(today)) },
    { label: "Last 7 days", range: joinRange(iso(addDays(today, -6)), iso(today)) },
    { label: "Last 30 days", range: joinRange(iso(addDays(today, -29)), iso(today)) },
    { label: "This month", range: joinRange(iso(new Date(today.getFullYear(), today.getMonth(), 1)), iso(today)) },
  ];
  const pick = (day: string) => {
    if (!draft) {
      setDraft(day);
      return;
    }
    const [a, b] = draft <= day ? [draft, day] : [day, draft];
    setDraft("");
    onChange(joinRange(a, b));
  };
  const title = month.toLocaleDateString(appLocale, { month: "long", year: "numeric" });
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-1.5">
        {presets.map((preset) => (
          <Button key={preset.label} variant={value === preset.range ? "secondary" : "outline"} size="sm" className="pointer-coarse:h-11" onClick={() => { setDraft(""); onChange(preset.range); }}>
            {preset.label}
          </Button>
        ))}
      </div>
      <div className="flex items-center justify-between">
        <Button variant="ghost" size="icon-sm" aria-label="Previous month" className="pointer-coarse:size-11" onClick={() => setMonth(new Date(month.getFullYear(), month.getMonth() - 1, 1))}><ChevronLeft /></Button>
        <span className="text-sm font-medium" aria-live="polite">{title}</span>
        <Button variant="ghost" size="icon-sm" aria-label="Next month" className="pointer-coarse:size-11" onClick={() => setMonth(new Date(month.getFullYear(), month.getMonth() + 1, 1))}><ChevronRight /></Button>
      </div>
      <div role="grid" aria-label={title} className="grid grid-cols-7 gap-y-0.5 text-center text-xs">
        {weekdays.map((day, i) => <span key={i} aria-hidden className="pb-1 text-muted-foreground">{day}</span>)}
        {monthGrid(month).map((date) => {
          const day = iso(date);
          const outside = date.getMonth() !== month.getMonth();
          const edge = day === start || day === end;
          const inside = !!start && !!end && day > start && day < end;
          return (
            <button
              key={day}
              type="button"
              role="gridcell"
              aria-selected={edge || inside}
              aria-label={date.toLocaleDateString(appLocale, { day: "numeric", month: "long", year: "numeric" })}
              onClick={() => pick(day)}
              className={cn(
                "mx-auto flex size-8 items-center justify-center rounded-md tabular-nums outline-none transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-signal/50 pointer-coarse:size-10",
                outside && "text-muted-foreground/50",
                inside && "rounded-none bg-signal/12",
                edge && "bg-signal font-semibold text-signal-foreground hover:bg-signal",
                day === iso(today) && !edge && "font-semibold text-signal",
              )}
            >
              {date.getDate()}
            </button>
          );
        })}
      </div>
      <p className="h-4 text-xs text-muted-foreground" aria-live="polite">{draft ? "Pick the last day" : start && end ? `${start} – ${end}` : "Pick the first day"}</p>
    </div>
  );
}
