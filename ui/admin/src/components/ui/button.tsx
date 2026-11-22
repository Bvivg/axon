import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-md font-medium transition-colors duration-[120ms] ease-signal disabled:cursor-not-allowed disabled:opacity-45 aria-busy:cursor-progress [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/88",
        secondary: "bg-secondary text-secondary-foreground hover:bg-accent",
        outline: "border border-border bg-card hover:bg-accent",
        ghost: "hover:bg-accent",
        destructive: "bg-destructive text-destructive-foreground hover:bg-destructive/90",
        "destructive-outline": "border border-border bg-card text-destructive hover:bg-accent",
        signal: "bg-signal text-signal-foreground hover:bg-signal/90",
        link: "h-auto px-0 underline decoration-border underline-offset-[3px] hover:decoration-foreground",
      },
      size: {
        sm: "h-7 px-2.5 text-xs",
        default: "h-8 px-3 text-sm",
        lg: "h-11 rounded-lg px-4 text-md md:h-9 md:rounded-md md:px-3.5 md:text-base",
        icon: "size-8 text-muted-foreground hover:text-foreground",
        "icon-sm": "size-7 text-muted-foreground hover:text-foreground",
        "icon-xs": "size-[22px] rounded-[5px] text-muted-foreground hover:text-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export type ButtonProps = ComponentProps<"button"> & VariantProps<typeof buttonVariants>;

export function Button({ className, variant, size, type = "button", ...props }: ButtonProps) {
  return (
    <button type={type} className={cn(buttonVariants({ variant, size }), className)} {...props} />
  );
}

export { buttonVariants };
