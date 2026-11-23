"use client";

import { ExternalLink } from "lucide-react";

import { buttonVariants } from "@/components/ui/button";
import { PaneHeader } from "@/components/ui/pane";

export const tvEmbedHref = "/embed/tv";

export const tvAppHref = "/games/tv";

export function LiveTv() {
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <PaneHeader title="Axon TV" back="/" backLabel="Back to administration"
        actions={<a href={tvAppHref} target="_blank" rel="noopener" className={buttonVariants({ variant: "ghost", size: "sm" })}><ExternalLink aria-hidden /> Open in Axon</a>} />
      <iframe src={tvEmbedHref} title="Axon TV" data-tv-frame className="min-h-0 w-full flex-1 border-0 bg-background" />
    </div>
  );
}
