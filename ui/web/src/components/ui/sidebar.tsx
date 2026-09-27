"use client";

import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { Separator as SeparatorPrimitive } from "@base-ui/react/separator";
import { PanelLeft } from "lucide-react";
import {
  cloneElement,
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ComponentProps,
  type CSSProperties,
  type ReactElement,
} from "react";

import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useIsMobile } from "@/lib/hooks/use-mobile";
import { cn } from "@/lib/utils";

type RenderProp = ReactElement<{ className?: string }>;

function mergeIntoRender(
  render: RenderProp,
  isActive: boolean,
  className: string,
  props: Record<string, unknown>,
) {
  return cloneElement(render, {
    "data-active": isActive,
    className: cn(className, render.props.className),
    ...props,
  } as Partial<{ className?: string }>);
}

const sidebarWidth = "16rem";
const sidebarWidthMobile = "18rem";
const sidebarWidthIcon = "3rem";
const sidebarKeyboardShortcut = "b";

type SidebarState = "expanded" | "collapsed";

interface SidebarContextValue {
  state: SidebarState;
  open: boolean;
  setOpen: (open: boolean) => void;
  openMobile: boolean;
  setOpenMobile: (open: boolean) => void;
  isMobile: boolean;
  toggleSidebar: () => void;
}

const SidebarContext = createContext<SidebarContextValue | null>(null);

export function useSidebar(): SidebarContextValue {
  const context = useContext(SidebarContext);
  if (!context) {
    throw new Error("useSidebar must be used inside a SidebarProvider");
  }
  return context;
}

export function SidebarProvider({
  defaultOpen = true,
  open: openProp,
  onOpenChange: setOpenProp,
  className,
  style,
  children,
  ...props
}: ComponentProps<"div"> & {
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const isMobile = useIsMobile();
  const [openMobile, setOpenMobile] = useState(false);
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const open = openProp ?? internalOpen;

  const setOpen = useCallback(
    (value: boolean) => {
      if (setOpenProp) {
        setOpenProp(value);
      } else {
        setInternalOpen(value);
      }
    },
    [setOpenProp],
  );

  const toggleSidebar = useCallback(() => {
    if (isMobile) {
      setOpenMobile((v) => !v);
    } else {
      setOpen(!open);
    }
  }, [isMobile, open, setOpen]);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === sidebarKeyboardShortcut && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        toggleSidebar();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [toggleSidebar]);

  const state: SidebarState = open ? "expanded" : "collapsed";

  const value = useMemo<SidebarContextValue>(
    () => ({ state, open, setOpen, openMobile, setOpenMobile, isMobile, toggleSidebar }),
    [state, open, setOpen, openMobile, isMobile, toggleSidebar],
  );

  return (
    <SidebarContext value={value}>
      <div
        style={{ "--sidebar-width": sidebarWidth, "--sidebar-width-icon": sidebarWidthIcon, ...style } as CSSProperties}
        className={cn("flex min-h-screen w-full", className)}
        {...props}
      >
        {children}
      </div>
    </SidebarContext>
  );
}

export function Sidebar({
  side = "left",
  variant = "sidebar",
  collapsible = "offcanvas",
  className,
  children,
  ...props
}: ComponentProps<"div"> & {
  side?: "left" | "right";
  variant?: "sidebar" | "floating" | "inset";
  collapsible?: "offcanvas" | "icon" | "none";
}) {
  const { isMobile, state, openMobile, setOpenMobile } = useSidebar();

  if (collapsible === "none") {
    return (
      <div
        className={cn("flex h-full w-(--sidebar-width) flex-col bg-sidebar text-sidebar-foreground", className)}
        {...props}
      >
        {children}
      </div>
    );
  }

  if (isMobile) {
    return (
      <DialogPrimitive.Root open={openMobile} onOpenChange={setOpenMobile}>
        <DialogPrimitive.Portal>
          <DialogPrimitive.Backdrop className="fixed inset-0 z-50 bg-black/70" />
          <DialogPrimitive.Popup
            className={cn(
              "fixed inset-y-0 z-50 flex w-(--sidebar-width-mobile) flex-col bg-sidebar text-sidebar-foreground outline-none",
              side === "left" ? "left-0 border-r border-sidebar-border" : "right-0 border-l border-sidebar-border",
            )}
            style={{ "--sidebar-width-mobile": sidebarWidthMobile } as CSSProperties}
          >
            {children}
          </DialogPrimitive.Popup>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    );
  }

  return (
    <div
      className="group peer hidden text-sidebar-foreground md:block"
      data-state={state}
      data-collapsible={state === "collapsed" ? collapsible : ""}
      data-variant={variant}
      data-side={side}
    >
      <div
        className={cn(
          "relative h-svh w-(--sidebar-width) bg-transparent transition-[width] duration-200 ease-linear",
          "group-data-[collapsible=offcanvas]:w-0",
          variant === "floating" || variant === "inset"
            ? "group-data-[collapsible=icon]:w-[calc(var(--sidebar-width-icon)+1rem)]"
            : "group-data-[collapsible=icon]:w-(--sidebar-width-icon)",
        )}
      />
      <div
        className={cn(
          "fixed inset-y-0 z-10 hidden h-svh w-(--sidebar-width) transition-[left,right,width] duration-200 ease-linear md:flex",
          side === "left"
            ? "left-0 group-data-[collapsible=offcanvas]:left-[calc(var(--sidebar-width)*-1)]"
            : "right-0 group-data-[collapsible=offcanvas]:right-[calc(var(--sidebar-width)*-1)]",
          variant === "floating" || variant === "inset"
            ? "p-2 group-data-[collapsible=icon]:w-[calc(var(--sidebar-width-icon)+1rem+2px)]"
            : "group-data-[collapsible=icon]:w-(--sidebar-width-icon) group-data-[side=left]:border-r group-data-[side=right]:border-l border-sidebar-border",
          className,
        )}
        {...props}
      >
        <div
          className={cn(
            "flex h-full w-full flex-col bg-sidebar",
            variant === "floating" && "rounded-lg border border-sidebar-border shadow-sm",
          )}
        >
          {children}
        </div>
      </div>
    </div>
  );
}

export function SidebarTrigger({ className, onClick, ...props }: ComponentProps<typeof Button>) {
  const { toggleSidebar } = useSidebar();

  return (
    <Button
      variant="ghost"
      size="icon"
      className={cn("size-7", className)}
      onClick={(event) => {
        onClick?.(event);
        toggleSidebar();
      }}
      {...props}
    >
      <PanelLeft />
      <span className="sr-only">Toggle sidebar</span>
    </Button>
  );
}

export function SidebarInset({ className, ...props }: ComponentProps<"main">) {
  return (
    <main
      className={cn(
        "relative flex min-h-svh flex-1 flex-col bg-background",
        "md:peer-data-[variant=inset]:m-2 md:peer-data-[variant=inset]:ml-0 md:peer-data-[variant=inset]:rounded-xl md:peer-data-[variant=inset]:shadow-sm md:peer-data-[state=collapsed]:peer-data-[variant=inset]:ml-2",
        className,
      )}
      {...props}
    />
  );
}

export function SidebarHeader({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-col gap-2 p-2", className)} {...props} />;
}

export function SidebarFooter({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-col gap-2 p-2", className)} {...props} />;
}

export function SidebarSeparator({ className, ...props }: ComponentProps<typeof SeparatorPrimitive>) {
  return <SeparatorPrimitive className={cn("mx-2 w-auto bg-sidebar-border", className)} {...props} />;
}

export function SidebarContent({ className, ...props }: ComponentProps<"div">) {
  return (
    <ScrollArea className="flex-1" viewportProps={{ className: "group-data-[collapsible=icon]:overflow-hidden!" }}>
      <div className={cn("flex min-h-full flex-col gap-2", className)} {...props} />
    </ScrollArea>
  );
}

export function SidebarGroup({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("relative flex w-full min-w-0 flex-col p-2", className)} {...props} />;
}

export function SidebarGroupLabel({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "flex h-8 shrink-0 items-center rounded-md px-2 text-xs font-medium text-sidebar-foreground/60 outline-none transition-[margin,opacity] duration-200 ease-linear",
        "group-data-[collapsible=icon]:-mt-8 group-data-[collapsible=icon]:opacity-0",
        className,
      )}
      {...props}
    />
  );
}

export function SidebarGroupContent({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("w-full text-sm", className)} {...props} />;
}

export function SidebarMenu({ className, ...props }: ComponentProps<"ul">) {
  return <ul className={cn("flex w-full min-w-0 flex-col gap-1", className)} {...props} />;
}

export function SidebarMenuItem({ className, ...props }: ComponentProps<"li">) {
  return <li className={cn("group/menu-item relative", className)} {...props} />;
}

export const sidebarMenuButtonClass =
  "peer/menu-button flex h-9 w-full items-center gap-2 overflow-hidden rounded-md p-2 text-left text-sm outline-none transition-[width,height,padding] hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring disabled:pointer-events-none disabled:opacity-50 data-[active=true]:bg-sidebar-accent data-[active=true]:font-medium data-[active=true]:text-sidebar-accent-foreground group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-0 [&>svg]:size-4 [&>svg]:shrink-0";

export function SidebarMenuButton({
  isActive = false,
  tooltip,
  className,
  render,
  ...props
}: ComponentProps<"button"> & {
  isActive?: boolean;
  tooltip?: string;
  render?: RenderProp;
}) {
  const { isMobile, state } = useSidebar();

  const className_ = cn(sidebarMenuButtonClass, className);

  const button = render ? (
    mergeIntoRender(render, isActive, className_, props)
  ) : (
    <button data-active={isActive} className={className_} {...props} />
  );

  if (!tooltip) {
    return button;
  }

  return (
    <Tooltip>
      <TooltipTrigger render={button} />
      <TooltipContent side="right" hidden={state !== "collapsed" || isMobile}>
        {tooltip}
      </TooltipContent>
    </Tooltip>
  );
}

export function SidebarMenuSub({ className, ...props }: ComponentProps<"ul">) {
  return (
    <ul
      className={cn(
        "ml-3.5 flex min-w-0 flex-col gap-1 border-l border-sidebar-border px-2.5 py-0.5",
        "group-data-[collapsible=icon]:hidden",
        className,
      )}
      {...props}
    />
  );
}

export function SidebarMenuSubItem({ className, ...props }: ComponentProps<"li">) {
  return <li className={cn("group/menu-sub-item relative", className)} {...props} />;
}

export function SidebarMenuSubButton({
  isActive = false,
  className,
  render,
  ...props
}: ComponentProps<"a"> & { isActive?: boolean; render?: RenderProp }) {
  const className_ = cn(
    "flex h-7 min-w-0 items-center gap-2 overflow-hidden rounded-md px-2 text-sm text-sidebar-foreground outline-none hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring data-[active=true]:bg-sidebar-accent data-[active=true]:text-sidebar-accent-foreground [&>svg]:size-4 [&>svg]:shrink-0",
    className,
  );

  if (render) {
    return mergeIntoRender(render, isActive, className_, props);
  }

  return <a data-active={isActive} className={className_} {...props} />;
}
