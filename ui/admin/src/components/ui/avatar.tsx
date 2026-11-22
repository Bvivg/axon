import Image from "next/image";
import type { ComponentProps, ReactNode } from "react";

import { cn } from "@/lib/utils";

export function Avatar({
  src,
  alt,
  fallback,
  className,
  sizes = "48px",
  priority,
  self = false,
  children,
  ...props
}: {
  src?: string;
  alt: string;
  fallback: string;
  sizes?: string;
  priority?: boolean;
  self?: boolean;
  children?: ReactNode;
} & Omit<ComponentProps<"div">, "children">) {
  return (
    <div
      className={cn(
        "relative flex shrink-0 items-center justify-center rounded-full font-semibold",
        self ? "bg-foreground text-background" : "bg-muted text-foreground",
        className,
      )}
      {...props}
    >
      {src ? (
        <span className="absolute inset-0 overflow-hidden rounded-[inherit]">
          <Image
            src={src}
            alt={alt}
            fill
            sizes={sizes}
            priority={priority}
            unoptimized
            className="object-cover"
          />
        </span>
      ) : (
        <span aria-hidden>{fallback}</span>
      )}
      {children}
    </div>
  );
}
