import { Check } from "lucide-react";

import { Avatar } from "@/components/ui/avatar";
import { PresenceDot } from "@/components/ui/status";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { initials, profileName } from "@/lib/format";
import { cn } from "@/lib/utils";

export function PersonRow({ profile, selected, onPick }: {
  profile: PublicProfile;
  selected?: boolean;
  onPick: (profile: PublicProfile) => void;
}) {
  const name = profileName(profile) || "No name";
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={() => onPick(profile)}
      className="flex min-h-14 w-full items-center gap-2.5 px-3 py-2 text-left transition-colors hover:bg-muted"
    >
      <Avatar src={profile.avatarUrls?.small || profile.avatarUrl} alt="" fallback={initials(name)} className="size-9" sizes="36px">
        {profile.online ? <PresenceDot online className="size-3" /> : null}
      </Avatar>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm font-medium">{name}</span>
        <span className="truncate text-xs text-muted-foreground">@{profile.username}</span>
      </span>
      {selected !== undefined ? (
        <span aria-hidden className={cn(
          "flex size-5 shrink-0 items-center justify-center rounded-full border transition-colors",
          selected ? "border-signal bg-signal text-signal-foreground" : "border-border",
        )}>
          {selected ? <Check className="size-3.5" /> : null}
        </span>
      ) : null}
    </button>
  );
}
