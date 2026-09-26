import type { ReactNode } from "react";

import { AuthHeader } from "@/components/auth/auth-header";

export default function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <div className="relative flex min-h-dvh flex-col">
      <div aria-hidden className="grid-paper pointer-events-none absolute inset-x-0 top-0 h-[260px] opacity-70" />
      <AuthHeader />
      <main className="relative mx-auto flex w-full max-w-[400px] flex-1 flex-col gap-6 px-5 pt-10 pb-8 md:pt-[12vh]">
        {children}
      </main>
    </div>
  );
}
