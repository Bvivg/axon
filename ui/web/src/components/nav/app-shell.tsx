"use client";

import { useEffect, type ReactNode } from "react";
import { usePathname, useSearchParams } from "next/navigation";

import { ChatActivityProvider } from "@/components/chat/chat-activity";
import { CallMiniBar } from "@/components/calls/call-mini-bar";
import { CallOverlay } from "@/components/calls/call-overlay";
import { CallProvider, useCall } from "@/components/calls/call-provider";
import { setSidebarMode, SidebarMode, useChatSidebar } from "@/components/chat/sidebar-store";
import { AppSidebar } from "@/components/nav/app-sidebar";
import { BrandMark } from "@/components/nav/brand";
import { ContextSidebar } from "@/components/nav/context-sidebar";
import { CommandPalette } from "@/components/nav/command-palette";
import { useGlobalShortcuts } from "@/components/nav/use-shortcuts";
import { SoundProvider } from "@/components/nav/sound-provider";
import { Skeleton } from "@/components/ui/skeleton";
import { LoadingDots } from "@/components/ui/status";
import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { useChatAddress } from "@/lib/chat/address";

export function AppShell({ children, callsEvidence = false }: { children: ReactNode; callsEvidence?: boolean }) {
  const { status, user } = useSession();

  useRequireSession();

  if (status !== "authenticated") {
    return <ShellSkeleton />;
  }

  return <SignedInShell key={user?.id} callsEvidence={callsEvidence}>{children}</SignedInShell>;
}

function SignedInShell({ children, callsEvidence }: { children: ReactNode; callsEvidence: boolean }) {
  useGlobalShortcuts();
  const pathname = usePathname();

  useEffect(() => {
    if (pathname !== "/chat") setSidebarMode(SidebarMode.Chats);
  }, [pathname]);

  return (
    <SoundProvider>
      <ChatActivityProvider>
        <CallProvider evidence={callsEvidence}>
          <CallWorkspace>{children}</CallWorkspace>
        </CallProvider>
      </ChatActivityProvider>
    </SoundProvider>
  );
}

function CallWorkspace({ children }: { children: ReactNode }) {
  const { expanded, expandedResult } = useCall();
  const { mode } = useChatSidebar();
  const pathname = usePathname();
  const inConversation = (useChatAddress() !== "" || pathname.startsWith("/chat/")) && mode === SidebarMode.Chats;
  const chatList = pathname === "/chat" && !inConversation;
  const search = useSearchParams();
  const sectionList = chatList || pathname === "/games" || (pathname === "/calls" && !search.has("call") && !search.has("view")) || (pathname === "/profile" && !search.has("view"));
  return (
    <>
      <div className="call-app-shell workspace-shell" data-conversation={inConversation} data-chat-list={chatList} data-section-list={sectionList}>
        <a href="#app-content" className="sr-only focus:not-sr-only focus:absolute focus:z-50 focus:m-2 focus:rounded-lg focus:bg-card focus:px-4 focus:py-2">Skip to content</a>
        <AppSidebar />
        <ContextSidebar />
        <div className="workspace-main">
          <div className="call-app-content workspace-content">
            <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
              <div id="app-content" tabIndex={-1} className="workspace-route flex min-h-0 min-w-0 flex-1 flex-col" inert={expanded || expandedResult}>{children}</div>
              <CallOverlay />
            </div>
            <CallMiniBar placement="app" />
          </div>
        </div>
      </div>
      <CommandPalette />
    </>
  );
}

function ShellSkeleton() {
  return (
    <div aria-busy="true" className="flex h-dvh overflow-hidden bg-background">
      <aside className="hidden w-16 shrink-0 flex-col gap-4 border-r border-border bg-sidebar px-2 py-3 md:flex">
        <div className="mx-auto flex h-11 items-center">
          <BrandMark className="size-10 rounded-xl" />
        </div>
        <div className="flex flex-col gap-3 px-2 pt-1.5">
          {[1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-10 w-full rounded-xl" />
          ))}
        </div>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex flex-1 items-center justify-center text-muted-foreground">
          <LoadingDots />
        </div>
      </div>
    </div>
  );
}
