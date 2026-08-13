import Image from "next/image";
import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export function Avatar({
  src,
  alt,
  fallback,
  className,
  sizes = "48px",
  priority,
  ...props
}: {
  src?: string;
  alt: string;
  fallback: string;
  sizes?: string;
  priority?: boolean;
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
        <Image
          src={src}
          alt={alt}
          fill
          sizes={sizes}
          priority={priority}
          unoptimized
          className="object-cover"
        />
      ) : (
        <span className="font-medium">{fallback}</span>
      )}
    </div>
  );
}
