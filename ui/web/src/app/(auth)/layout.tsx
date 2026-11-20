import { Suspense, type ReactNode } from "react";

import { AuthHeader } from "@/components/auth/auth-header";

export default function AuthLayout({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-dvh flex-col bg-canvas">
      <Suspense><AuthHeader /></Suspense>
      <main className="flex flex-1 items-start justify-center px-4 pt-6 pb-10 sm:items-center sm:pt-0">
        <div className="grid w-full max-w-[400px] gap-4 rounded-[18px] border border-line bg-surface p-6 sm:p-8 [&_h1]:text-xl [&_h1]:font-semibold [&_h1]:tracking-snug">
          {children}
        </div>
      </main>
    </div>
  );
}
