import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";

export default function NotFound() {
  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-[420px] flex-col justify-center gap-4 px-6 py-8">
      <span aria-hidden className="font-mono text-[56px] leading-[56px] tracking-display text-border">
        404
      </span>
      <h1 className="text-2xl font-semibold tracking-tight">This page doesn’t exist</h1>
      <div className="flex flex-col gap-2 pt-2">
        <Link href="/" className={buttonVariants({ size: "lg", className: "w-full" })}>
          Go to lobby
        </Link>
        <Link href="/chat" className={buttonVariants({ variant: "outline", size: "lg", className: "w-full" })}>
          Open chats
        </Link>
      </div>
    </main>
  );
}
