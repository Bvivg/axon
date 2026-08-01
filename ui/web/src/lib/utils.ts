import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * Joins class names, letting a later Tailwind utility win over an earlier one
 * in the same group. Without the merge, `cn("p-2", "p-4")` emits both and the
 * winner is decided by stylesheet order rather than by the caller.
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
