import { cva, type VariantProps } from "class-variance-authority";
import { CircleAlert, TriangleAlert } from "lucide-react";
import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

const alertVariants = cva(
  "flex items-start gap-2.5 rounded-lg border px-3 py-2.5 text-sm [&>svg]:mt-px [&>svg]:size-4 [&>svg]:shrink-0",
  {
    variants: {
      variant: {
        destructive:
          "border-destructive/35 bg-destructive/5 text-foreground [&>svg]:text-destructive",
        warning: "border-warning/50 bg-warning/14 text-foreground [&>svg]:text-warning",
      },
    },
    defaultVariants: { variant: "destructive" },
  },
);

export function Alert({
  className,
  variant,
  children,
  ...props
}: ComponentProps<"div"> & VariantProps<typeof alertVariants>) {
  const Icon = variant === "warning" ? TriangleAlert : CircleAlert;
  return (
    <div role="alert" className={cn(alertVariants({ variant }), className)} {...props}>
      <Icon aria-hidden />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

export function FieldError({ className, children, ...props }: ComponentProps<"p">) {
  return (
    <p
      className={cn("flex items-center gap-1.5 text-sm text-destructive [&>svg]:size-3.5", className)}
      {...props}
    >
      <CircleAlert aria-hidden className="shrink-0" />
      {children}
    </p>
  );
}
