"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";

import { nextParam } from "@/lib/auth/guards";

export function AuthSwitch({ prompt, href, label }: { prompt: string; href: string; label: string }) {
  const next = useSearchParams().get(nextParam);
  return (
    <p className="text-center text-sm text-muted-foreground">
      {prompt}{" "}
      <Link href={next ? { pathname: href, query: { [nextParam]: next } } : href} className="rounded-sm font-medium text-signal underline-offset-4 hover:underline">
        {label}
      </Link>
    </p>
  );
}
