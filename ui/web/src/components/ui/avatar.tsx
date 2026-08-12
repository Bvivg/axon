import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export function Avatar({
  src,
  alt,
  fallback,
  className,
  ...props
}: {
  src?: string;
  alt: string;
  fallback: string;
} & Omit<ComponentProps<"div">, "children">) {
  return (
    <div
      className={cn(
        "relative flex shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted text-muted-foreground",
        className,
      )}
      {...props}
    >
      {src ? (
        <img src={src} alt={alt} className="h-full w-full object-cover" />
      ) : (
        <span className="font-medium">{fallback}</span>
      )}
    </div>
  );
}
