import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

export function EmptyState({
  icon: Icon,
  title,
  children,
  className,
  headingLevel = "h2",
}: {
  icon: LucideIcon;
  title: string;
  children?: ReactNode;
  className?: string;
  headingLevel?: "h1" | "h2";
}) {
  const Heading = headingLevel;
  return (
    <section
      className={cn("flex flex-col items-center gap-3 px-6 py-8 text-center", className)}
    >
      <span className="flex size-10 items-center justify-center rounded-lg border border-border text-muted-foreground">
        <Icon aria-hidden className="size-[18px]" strokeWidth={1.5} />
      </span>
      <Heading className="text-base font-semibold">{title}</Heading>
      {children ? <div className="flex flex-wrap justify-center gap-2 pt-1">{children}</div> : null}
    </section>
  );
}
