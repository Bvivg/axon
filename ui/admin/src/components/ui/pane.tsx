import { ChevronLeft } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

import { buttonVariants } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";

export function PaneHeader({
  title,
  titleAs: Title = "h1",
  subtitle,
  leading,
  actions,
  back,
  backLabel = "Back",
  className,
  children,
}: {
  title?: ReactNode;
  titleAs?: "h1" | "p";
  subtitle?: ReactNode;
  leading?: ReactNode;
  actions?: ReactNode;
  back?: string;
  backLabel?: string;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <header className={cn("console-page-header", className)}>
      {children ?? (
        <>
          {back ? (
            <Link
              href={back}
              aria-label={backLabel}
              className={cn(buttonVariants({ variant: "ghost", size: "icon" }), "-ml-2 -mr-1 shrink-0 min-[721px]:hidden")}
            >
              <ChevronLeft className="size-5" />
            </Link>
          ) : null}
          {leading}
          <div className="min-w-0 flex-1">
            <Title className="truncate text-[13px] font-semibold">{title}</Title>
            {subtitle ? <p className="truncate text-xs text-muted-foreground">{subtitle}</p> : null}
          </div>
          {actions ? <div className="flex shrink-0 items-center gap-1">{actions}</div> : null}
        </>
      )}
    </header>
  );
}

export function Pane({
  title,
  titleAs,
  subtitle,
  leading,
  actions,
  back,
  backLabel,
  width = "default",
  children,
}: {
  title: ReactNode;
  titleAs?: "h1" | "p";
  subtitle?: ReactNode;
  leading?: ReactNode;
  actions?: ReactNode;
  back?: string;
  backLabel?: string;
  width?: "narrow" | "default" | "wide";
  children: ReactNode;
}) {
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <PaneHeader title={title} titleAs={titleAs} subtitle={subtitle} leading={leading} actions={actions} back={back} backLabel={backLabel} />
      <ScrollArea className="min-h-0 flex-1">
        <div
          className={cn(
            "mx-auto w-full space-y-4 px-4 py-5 sm:px-6",
            width === "narrow" && "max-w-[720px]",
            width === "default" && "max-w-[960px]",
            width === "wide" && "max-w-[1200px]",
          )}
        >
          {children}
        </div>
      </ScrollArea>
    </div>
  );
}

export function PaneSection({ id, title, aside, children, className }: { id: string; title: ReactNode; aside?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section aria-labelledby={id} className={cn("flex flex-col gap-4 rounded-xl border border-line bg-surface p-4", className)}>
      <div className="flex items-center justify-between gap-3">
        <h2 id={id} className="text-sm font-semibold">{title}</h2>
        {aside}
      </div>
      {children}
    </section>
  );
}
