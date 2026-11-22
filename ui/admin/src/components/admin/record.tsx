"use client";

import { Check, Copy, type LucideIcon } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Pane } from "@/components/ui/pane";
import { Skeleton } from "@/components/ui/skeleton";
import { shortID } from "@/lib/format";
import { cn } from "@/lib/utils";

export function RecordPage({
  section,
  back,
  backLabel,
  media,
  title,
  lines,
  tags,
  actions,
  facts,
  children,
}: {
  section: string;
  back: string;
  backLabel?: string;
  media?: ReactNode;
  title: ReactNode;
  lines?: ReactNode;
  tags?: ReactNode;
  actions?: ReactNode;
  facts?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <Pane title={section} titleAs="p" back={back} backLabel={backLabel} width="wide">
      <div className="flex flex-col gap-7 pt-2 pb-10">
        <header className="flex flex-col gap-5 border-b border-line pb-6 md:flex-row md:items-start">
          <div className="flex min-w-0 flex-1 items-start gap-4">
            {media ? <div className="shrink-0">{media}</div> : null}
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <h1 className="text-[26px] leading-[1.15] font-semibold tracking-[-0.015em] break-words">{title}</h1>
              {lines ? <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">{lines}</div> : null}
              {tags ? <div className="flex flex-wrap items-center gap-1.5 pt-1.5">{tags}</div> : null}
            </div>
          </div>
          {actions ? <div className="flex flex-wrap items-center gap-2 md:max-w-[50%] md:justify-end md:pt-1">{actions}</div> : null}
        </header>
        <div className="grid items-start gap-x-10 gap-y-8 lg:grid-cols-[minmax(0,1fr)_300px]">
          <div className="flex min-w-0 flex-col gap-9 lg:col-start-1 lg:row-start-1">{children}</div>
          {facts ? <aside aria-label="Details" className="min-w-0 lg:sticky lg:top-0 lg:col-start-2 lg:row-start-1">{facts}</aside> : null}
        </div>
      </div>
    </Pane>
  );
}

export function RecordState({ section, back, backLabel, children }: { section: string; back: string; backLabel?: string; children: ReactNode }) {
  return (
    <Pane title={section} back={back} backLabel={backLabel} width="wide">
      <div className="pt-2">{children}</div>
    </Pane>
  );
}

export function RecordLoading({ section, back, backLabel, label }: { section: string; back: string; backLabel?: string; label: string }) {
  return (
    <Pane title={section} back={back} backLabel={backLabel} width="wide">
      <div role="status" aria-label={label} className="flex flex-col gap-8 pt-2">
        <div className="flex items-center gap-4 border-b border-line pb-7">
          <Skeleton className="size-16 rounded-2xl" />
          <div className="flex flex-1 flex-col gap-2"><Skeleton className="h-7 w-64" /><Skeleton className="h-4 w-40" /></div>
        </div>
        <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_300px]">
          <div className="flex flex-col gap-3"><Skeleton className="h-5 w-32" /><Skeleton className="h-40 w-full rounded-xl" /></div>
          <Skeleton className="h-56 w-full rounded-xl" />
        </div>
      </div>
    </Pane>
  );
}

export function RecordIcon({ icon: Icon, tone = "signal" }: { icon: LucideIcon; tone?: "signal" | "muted" | "destructive" }) {
  return (
    <span
      aria-hidden
      className={cn(
        "grid size-16 place-items-center rounded-2xl border",
        tone === "signal" && "border-signal/25 bg-signal/10 text-signal",
        tone === "muted" && "border-line bg-surface-raised text-muted-foreground",
        tone === "destructive" && "border-destructive/25 bg-destructive/10 text-destructive",
      )}
    >
      <Icon className="size-7" strokeWidth={1.75} />
    </span>
  );
}

export function RecordSection({ id, title, count, aside, children }: { id: string; title: ReactNode; count?: number; aside?: ReactNode; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="flex min-w-0 flex-col gap-3">
      <div className="flex min-h-8 flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <h2 id={id} className="flex items-baseline gap-2 text-[15px] font-semibold">
          {title}
          {count === undefined ? null : <span className="text-sm font-normal tabular-nums text-muted-foreground">{count.toLocaleString()}</span>}
        </h2>
        {aside}
      </div>
      {children}
    </section>
  );
}

export function RecordFrame({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("min-w-0 overflow-hidden rounded-xl border border-line bg-surface", className)}>{children}</div>;
}

export function RecordRows({ label, children }: { label?: string; children: ReactNode }) {
  return <ul aria-label={label} className="divide-y divide-line">{children}</ul>;
}

export function RecordRow({ children, aside, className }: { children: ReactNode; aside?: ReactNode; className?: string }) {
  return (
    <li className={cn("flex min-h-14 items-center justify-between gap-4 px-4 py-2.5", className)}>
      <div className="flex min-w-0 flex-1 items-center gap-3">{children}</div>
      {aside ? <div className="flex shrink-0 items-center gap-2">{aside}</div> : null}
    </li>
  );
}

export function RecordEmpty({ children }: { children: ReactNode }) {
  return <p className="px-4 py-7 text-center text-sm text-muted-foreground">{children}</p>;
}

export function RecordNote({ tone = "muted", children }: { tone?: "muted" | "destructive"; children: ReactNode }) {
  return (
    <div
      className={cn(
        "rounded-xl border px-4 py-3 text-sm",
        tone === "muted" && "border-line bg-surface-raised text-muted-foreground",
        tone === "destructive" && "border-destructive/30 bg-destructive/[0.07]",
      )}
    >
      {children}
    </div>
  );
}

export function Facts({ children }: { children: ReactNode }) {
  return <dl className="grid grid-cols-2 gap-x-4 rounded-xl border border-line bg-surface px-4 py-2 sm:grid-cols-3 lg:flex lg:flex-col lg:py-1">{children}</dl>;
}

export function Fact({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 py-2 lg:min-h-11 lg:flex-row lg:items-center lg:justify-between lg:gap-4 lg:border-b lg:border-line lg:last:border-b-0">
      <dt className="shrink-0 text-xs text-muted-foreground lg:text-[13px]">{term}</dt>
      <dd className="flex min-h-7 min-w-0 items-center text-[13px] break-words lg:justify-end lg:text-right">{children}</dd>
    </div>
  );
}

export function CopyID({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  };
  return (
    <span className="inline-flex items-center gap-0.5">
      <span className="font-mono text-xs text-muted-foreground" title={value}>{shortID(value)}</span>
      <Button variant="ghost" size="icon-sm" aria-label={copied ? `${label} copied` : `Copy ${label}`} onClick={() => void copy()}>
        {copied ? <Check className="text-success" /> : <Copy />}
      </Button>
    </span>
  );
}
