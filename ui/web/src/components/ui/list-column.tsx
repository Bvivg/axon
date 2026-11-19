import type { ReactNode } from "react";

export function ListColumnHeader({ title, back, action }: { title: ReactNode; back?: ReactNode; action?: ReactNode }) {
  return (
    <header className="mb-2 flex h-11 shrink-0 items-center gap-2 px-3">
      {back}
      <h1 className="flex-1 truncate text-lg font-semibold tracking-tight">{title}</h1>
      {action}
    </header>
  );
}
