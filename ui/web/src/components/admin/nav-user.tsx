"use client";

import { ArrowLeftRight, ChevronsUpDown, LogOut, Monitor, Moon, Sun } from "lucide-react";
import Link from "next/link";

import { Avatar } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuLinkItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarFooter, SidebarMenu, SidebarMenuItem, sidebarMenuButtonClass } from "@/components/ui/sidebar";
import { useSession } from "@/lib/auth/session";
import { setTheme, Theme, useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

export function NavUser() {
  const { user, signOut } = useSession();
  const theme = useTheme();

  if (!user) {
    return null;
  }

  const fullName = [user.firstName, user.lastName].filter(Boolean).join(" ");
  const displayName = user.nickname || fullName || user.displayName || user.email;
  const avatarSrc = user.avatarUrls?.small || user.avatarUrl;
  const initial = displayName.slice(0, 1).toUpperCase();

  return (
    <SidebarFooter>
      <SidebarMenu>
        <SidebarMenuItem>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button type="button" className={cn(sidebarMenuButtonClass, "h-12")}>
                  <Avatar src={avatarSrc} alt="" fallback={initial} className="size-8" sizes="32px" />
                  <span className="grid min-w-0 flex-1 text-left leading-tight group-data-[collapsible=icon]:hidden">
                    <span className="truncate text-sm font-medium">{displayName}</span>
                    <span className="truncate text-xs text-sidebar-foreground/60">{user.email}</span>
                  </span>
                  <ChevronsUpDown className="ml-auto size-4 group-data-[collapsible=icon]:hidden" />
                </button>
              }
            />
            <DropdownMenuContent side="top" align="start" sideOffset={8} className="w-64">
              <DropdownMenuLabel>
                <div className="flex items-center gap-2">
                  <Avatar src={avatarSrc} alt="" fallback={initial} className="size-9" sizes="36px" />
                  <div className="grid min-w-0 flex-1">
                    <span className="truncate text-sm font-medium">{fullName || displayName}</span>
                    <span className="truncate text-xs text-muted-foreground">{user.email}</span>
                  </div>
                </div>
                {user.roles.length > 0 ? (
                  <div className="mt-2 flex flex-wrap gap-1">
                    {user.roles.map((role) => (
                      <span
                        key={role}
                        className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground"
                      >
                        {role}
                      </span>
                    ))}
                  </div>
                ) : null}
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuRadioGroup
                value={theme}
                onValueChange={(value) => setTheme(value as Theme)}
              >
                <DropdownMenuRadioItem value={Theme.Light}>
                  <Sun />
                  Light
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value={Theme.Dark}>
                  <Moon />
                  Dark
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value={Theme.System}>
                  <Monitor />
                  System
                </DropdownMenuRadioItem>
              </DropdownMenuRadioGroup>
              <DropdownMenuSeparator />
              <DropdownMenuLinkItem render={<Link href="/" />}>
                <ArrowLeftRight />
                Back to app
              </DropdownMenuLinkItem>
              <DropdownMenuItem onClick={() => void signOut()}>
                <LogOut />
                Sign out
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </SidebarMenuItem>
      </SidebarMenu>
    </SidebarFooter>
  );
}
