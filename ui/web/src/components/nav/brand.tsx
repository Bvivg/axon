import { cn } from "@/lib/utils";

export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        "flex size-[22px] shrink-0 items-center justify-center rounded-md bg-foreground",
        className,
      )}
    >
      <svg viewBox="0 0 22 22" className="size-[64%]">
        <circle cx="5" cy="11" r="3" fill="var(--color-background)" />
        <path d="M8 11h6" stroke="var(--color-background)" strokeWidth="2" />
        <circle cx="17" cy="11" r="3" fill="var(--color-signal)" />
      </svg>
    </span>
  );
}
