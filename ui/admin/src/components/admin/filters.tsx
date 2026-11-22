"use client";

import { Search } from "lucide-react";
import { useEffect, useId, useRef, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

export function Toolbar({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div role="search" className={cn("flex shrink-0 flex-wrap items-center gap-2 border-b border-line bg-surface px-4 py-2.5", className)}>
      {children}
    </div>
  );
}

export function SearchFilter({ label, value, onChange, placeholder, className }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string; className?: string }) {
  const id = useId();
  const [text, setText] = useState(value);
  const [seen, setSeen] = useState(value);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const committed = useRef(value);
  if (value !== seen) {
    setSeen(value);
    if (value !== text.trim()) setText(value);
  }
  useEffect(() => {
    if (value === committed.current) return;
    committed.current = value;
    clearTimeout(timer.current);
  }, [value]);
  useEffect(() => () => clearTimeout(timer.current), []);
  const type = (next: string) => {
    setText(next);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      committed.current = next.trim();
      onChange(next.trim());
    }, 300);
  };
  return (
    <div className={cn("relative min-w-0 flex-[1_1_14rem]", className)}>
      <label htmlFor={id} className="sr-only">{label}</label>
      <Search aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input id={id} type="search" className="pl-8" placeholder={placeholder ?? label} value={text} onChange={(event) => type(event.target.value)} />
    </div>
  );
}

export interface Option {
  value: string;
  label: string;
}

export function ChipFilter({ label, value, options, onChange }: { label: string; value: string; options: Option[]; onChange: (value: string) => void }) {
  return (
    <div role="group" aria-label={label} className="flex flex-wrap gap-1">
      {options.map((option) => (
        <Button
          key={option.value || "all"}
          variant={value === option.value ? "secondary" : "ghost"}
          size="sm"
          className="rounded-full pointer-coarse:min-h-11"
          aria-pressed={value === option.value}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </Button>
      ))}
    </div>
  );
}

export function SelectFilter({ label, value, options, onChange }: { label: string; value: string; options: Option[]; onChange: (value: string) => void }) {
  const id = useId();
  return (
    <div className="flex items-center gap-1.5">
      <label htmlFor={id} className="text-xs text-muted-foreground">{label}</label>
      <select
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="h-11 rounded-lg border border-input bg-background px-2 text-[16px] focus-visible:border-signal focus-visible:outline-none md:h-8 md:rounded-md md:text-sm"
      >
        {options.map((option) => <option key={option.value || "all"} value={option.value}>{option.label}</option>)}
      </select>
    </div>
  );
}

export function TextFilter({ label, value, onChange, placeholder, type = "text" }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string; type?: "text" | "date" }) {
  const id = useId();
  const [text, setText] = useState(value);
  const [seen, setSeen] = useState(value);
  if (value !== seen) {
    setSeen(value);
    setText(value);
  }
  const commit = () => {
    if (text.trim() !== value) onChange(text.trim());
  };
  return (
    <div className="flex items-center gap-1.5">
      <label htmlFor={id} className="text-xs text-muted-foreground">{label}</label>
      <Input
        id={id}
        type={type}
        className={type === "date" ? "w-auto" : "w-[18ch] font-mono text-xs md:text-xs"}
        placeholder={placeholder}
        value={text}
        onChange={(event) => {
          setText(event.target.value);
          if (type === "date") onChange(event.target.value);
        }}
        onBlur={commit}
        onKeyDown={(event) => {
          if (event.key === "Enter") commit();
        }}
      />
    </div>
  );
}
