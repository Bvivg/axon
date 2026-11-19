import { cn } from "@/lib/utils";

export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        "flex size-[22px] shrink-0 items-center justify-center rounded-md border border-line bg-surface text-foreground",
        className,
      )}
    >
      <svg viewBox="40 140 432 232" className="w-[78%]">
        <circle cx="110" cy="256" r="56" className="fill-signal" />
        <rect x="184" y="238" width="50" height="36" rx="18" fill="currentColor" />
        <rect x="248" y="238" width="50" height="36" rx="18" fill="currentColor" />
        <g fill="none" stroke="currentColor" strokeWidth="22" strokeLinecap="round">
          <path d="M316 256C358 256 370 162 406 162" />
          <path d="M316 256H434" />
          <path d="M316 256C358 256 370 350 406 350" />
        </g>
        <circle cx="406" cy="162" r="24" fill="currentColor" />
        <circle cx="434" cy="256" r="24" fill="currentColor" />
        <circle cx="406" cy="350" r="24" fill="currentColor" />
      </svg>
    </span>
  );
}
