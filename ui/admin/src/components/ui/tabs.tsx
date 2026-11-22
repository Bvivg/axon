"use client";

import { Tabs as BaseTabs } from "@base-ui/react/tabs";

import { cn } from "@/lib/utils";

export const Tabs = BaseTabs.Root;
export const TabsContent = BaseTabs.Panel;

export function TabsList({ className, ...props }: Omit<BaseTabs.List.Props, "className"> & { className?: string }) {
  return <BaseTabs.List className={cn("inline-flex items-center gap-0.5 rounded-lg bg-muted p-0.5", className)} {...props} />;
}

export function TabsTrigger({ className, ...props }: Omit<BaseTabs.Tab.Props, "className"> & { className?: string }) {
  return (
    <BaseTabs.Tab
      className={cn("min-h-8 rounded-md px-2.5 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground aria-selected:bg-card aria-selected:text-foreground aria-selected:shadow-raised pointer-coarse:min-h-11", className)}
      {...props}
    />
  );
}
