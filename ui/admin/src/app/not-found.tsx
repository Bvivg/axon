import { FileQuestion } from "lucide-react";
import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";

export default function NotFound() {
  return (
    <main className="flex min-h-dvh items-center justify-center p-4">
      <EmptyState icon={FileQuestion} title="Page not found" headingLevel="h1">
        <Link href="/" className={buttonVariants({ variant: "outline" })}>Back to overview</Link>
      </EmptyState>
    </main>
  );
}
