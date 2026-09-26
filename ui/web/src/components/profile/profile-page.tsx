import { ChevronLeft } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

export function ProfilePage({
  title,
  aside,
  children,
}: {
  title: string;
  aside?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col px-4 pb-12 md:px-8 md:pt-10">
      <Link
        href="/profile"
        className="-ml-2 flex h-12 w-fit items-center gap-1 pr-2 text-sm text-muted-foreground md:hidden"
      >
        <ChevronLeft aria-hidden className="size-5" strokeWidth={1.5} />
        You
      </Link>
      <div className="flex items-baseline justify-between gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {aside}
      </div>
      {children}
    </div>
  );
}

export function ProfileSection({
  id,
  label,
  aside,
  children,
}: {
  id: string;
  label: string;
  aside?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section aria-labelledby={id} className="flex flex-col gap-4 border-t border-border py-6 first-of-type:border-t-0">
      <div className="flex items-center justify-between gap-3">
        <h2 id={id} className="eyebrow">
          {label}
        </h2>
        {aside}
      </div>
      {children}
    </section>
  );
}
