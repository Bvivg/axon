"use client";

import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

import { ChatActivityProvider } from "@/components/chat/chat-activity";
import { ComposerDialog } from "@/components/chat/composer-dialog";
import { AppSidebar } from "@/components/nav/app-sidebar";
import { BrandMark } from "@/components/nav/brand";
import { CommandPalette } from "@/components/nav/command-palette";
import { isConversation } from "@/components/nav/items";
import { Tabbar } from "@/components/nav/tabbar";
import { Topbar } from "@/components/nav/topbar";
import { useGlobalShortcuts } from "@/components/nav/use-shortcuts";
import { Skeleton } from "@/components/ui/skeleton";
import { LoadingDots } from "@/components/ui/status";
import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { cn } from "@/lib/utils";

export function AppShell({ children }: { children: ReactNode }) {
  const { status } = useSession();

  useRequireSession();

  if (status !== "authenticated") {
    return <ShellSkeleton />;
  }

  return <SignedInShell>{children}</SignedInShell>;
}

function SignedInShell({ children }: { children: ReactNode }) {
  useGlobalShortcuts();
  const pathname = usePathname();

  return (
    <ChatActivityProvider>
      <div className="flex h-dvh overflow-hidden bg-background">
        <AppSidebar />
        <div className="flex min-w-0 flex-1 flex-col">
          <Topbar />
          <div
            className={cn(
              "flex min-h-0 flex-1 flex-col pb-28 lg:pb-0",
              isConversation(pathname) && "max-md:pb-[env(safe-area-inset-bottom)]",
            )}
          >
            {children}
          </div>
          <Tabbar />
        </div>
      </div>
      <CommandPalette />
      <ComposerDialog />
    </ChatActivityProvider>
  );
}

function ShellSkeleton() {
  return (
    <div aria-busy="true" className="flex h-dvh overflow-hidden bg-background">
      <aside className="hidden w-62 shrink-0 flex-col gap-[18px] border-r border-border bg-sidebar px-2 py-2.5 lg:flex">
        <div className="flex h-9 items-center gap-2.5 px-2">
          <BrandMark />
          <span className="text-sm font-semibold">Axon</span>
        </div>
        <div className="flex flex-col gap-3 px-2 pt-1.5">
          {[64, 56, 48].map((width) => (
            <Skeleton key={width} className="h-3" style={{ width: `${width}%` }} />
          ))}
        </div>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="hidden h-[52px] shrink-0 items-center border-b border-border px-5 md:flex">
          <Skeleton className="h-3 w-24" />
        </div>
        <div className="flex flex-1 items-center justify-center text-muted-foreground">
          <LoadingDots />
        </div>
      </div>
    </div>
  );
}
