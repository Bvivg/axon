"use client";

import { ChevronsUpDown, IdCard, LogOut, MonitorSmartphone, Palette, ShieldCheck } from "lucide-react";
import Link from "next/link";

import { Avatar } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLinkItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { PresenceDot } from "@/components/ui/status";
import { useSession } from "@/lib/auth/session";
import { initials, personName } from "@/lib/format";
import { ConnectionStatus, useConnectionStatus } from "@/lib/ws/connection";

export function AccountMenu() {
  const { user, signOut } = useSession();
  const connection = useConnectionStatus();

  if (!user) {
    return null;
  }

  const name = personName(user);
  const online = connection === ConnectionStatus.Connected;
  const handle = user.nickname ? `@${user.nickname}` : user.email;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label="Account"
            className="flex h-11 w-full items-center gap-2.5 rounded-md px-2 text-left transition-colors duration-[120ms] ease-signal hover:bg-accent data-[popup-open]:bg-accent"
          >
            <Avatar
              self
              src={user.avatarUrls?.small || user.avatarUrl}
              alt=""
              fallback={initials(name)}
              sizes="28px"
              className="size-7 text-2xs"
            >
              <PresenceDot online={online} className="-right-px -bottom-px size-[9px] ring-sidebar" />
            </Avatar>
            <span className="flex min-w-0 flex-1 flex-col">
              <span className="truncate text-sm font-medium">{name}</span>
              <span className="truncate font-mono text-2xs leading-[14px] text-muted-foreground">
                {online ? `${handle} · online` : handle}
              </span>
            </span>
            <ChevronsUpDown aria-hidden className="size-3.5 shrink-0 text-muted-foreground" strokeWidth={1.5} />
          </button>
        }
      />
      <DropdownMenuContent side="top" align="start" className="w-[232px]">
        <DropdownMenuLinkItem render={<Link href="/profile/my" />}>
          <IdCard />
          Personal info
        </DropdownMenuLinkItem>
        <DropdownMenuLinkItem render={<Link href="/profile/sessions" />}>
          <MonitorSmartphone />
          Sessions
        </DropdownMenuLinkItem>
        <DropdownMenuLinkItem render={<Link href="/profile/appearance" />}>
          <Palette />
          Appearance
        </DropdownMenuLinkItem>
        {user.roles.includes("admin") ? (
          <DropdownMenuLinkItem render={<Link href="/admin" />}>
            <ShieldCheck />
            Admin
          </DropdownMenuLinkItem>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => void signOut()}>
          <LogOut />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
