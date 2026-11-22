import type { LucideIcon } from "lucide-react";
import Link from "next/link";
import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/lib/utils";

type ListItemProps = {
  active?: boolean;
  leading?: ReactNode;
  title: ReactNode;
  meta?: ReactNode;
  subtitle?: ReactNode;
  trailing?: ReactNode;
  className?: string;
};

const rowClass = "mx-1.5 flex min-h-[52px] items-center gap-2 rounded-[10px] px-2 py-1.5 text-start transition-colors";

function Body({ title, meta, subtitle, trailing }: Pick<ListItemProps, "title" | "meta" | "subtitle" | "trailing">) {
  return (
    <div className="min-w-0 flex-1">
      <div className="flex items-baseline justify-between gap-2">
        <span className="truncate text-xs font-semibold text-foreground">{title}</span>
        {meta ? <span className="shrink-0 text-[10px] text-muted-foreground">{meta}</span> : null}
      </div>
      {subtitle || trailing ? (
        <div className="flex items-center gap-1">
          <div className="flex min-w-0 flex-1 items-center gap-1 truncate text-[11px] text-muted-foreground">{subtitle}</div>
          {trailing}
        </div>
      ) : null}
    </div>
  );
}

export function ListItem({
  href,
  active = false,
  leading,
  title,
  meta,
  subtitle,
  trailing,
  className,
  ...props
}: ListItemProps & Omit<ComponentProps<typeof Link>, "title" | "className">) {
  return (
    <Link
      href={href}
      aria-current={active ? "page" : undefined}
      className={cn(rowClass, active ? "bg-accent" : "hover:bg-muted", className)}
      {...props}
    >
      {leading ? <span className="relative shrink-0">{leading}</span> : null}
      <Body title={title} meta={meta} subtitle={subtitle} trailing={trailing} />
    </Link>
  );
}

export function ListIcon({ icon: Icon, className }: { icon: LucideIcon; className?: string }) {
  return (
    <span className={cn("flex size-8 items-center justify-center rounded-[10px] bg-accent text-signal", className)}>
      <Icon aria-hidden className="size-4" strokeWidth={1.8} />
    </span>
  );
}

export function ListBadge({ children }: { children: ReactNode }) {
  return (
    <span className="ml-1 flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-signal px-1.5 text-[11px] font-semibold text-signal-foreground">
      {children}
    </span>
  );
}

export function ListLabel({ children }: { children: ReactNode }) {
  return <h2 className="eyebrow flex h-8 items-center px-4">{children}</h2>;
}
