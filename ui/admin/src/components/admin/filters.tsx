"use client";

import { Search } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";

import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

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
      <Input id={id} type="search" className="pl-8 md:pl-8 pointer-coarse:h-11" placeholder={placeholder ?? label} value={text} onChange={(event) => type(event.target.value)} />
    </div>
  );
}
