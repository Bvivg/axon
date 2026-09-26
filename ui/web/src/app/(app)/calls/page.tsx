import { MessageSquare, PhoneOff } from "lucide-react";
import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";

export default function CallsPage() {
  return (
    <main className="flex flex-1 items-center justify-center p-4 md:p-8">
      <EmptyState
        headingLevel="h1"
        icon={PhoneOff}
        title="No calls yet"
        className="w-full max-w-md rounded-lg border border-dashed border-border py-8"
      >
        <Link href="/chat" className={buttonVariants({ variant: "outline" })}>
          <MessageSquare strokeWidth={1.5} />
          Open chats
        </Link>
      </EmptyState>
    </main>
  );
}
