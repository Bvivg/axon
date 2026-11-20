"use client";

import type { ReactNode } from "react";

import { CallVideo, useVideoAvailable } from "@/components/calls/remote-media";
import { Avatar } from "@/components/ui/avatar";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { CallMode, type Call } from "@/gen/axon/calls/v1/calls_pb";
import type { CallView } from "@/lib/calls/manager";
import { isSfuMedia, remoteParticipant } from "@/lib/calls/media-kind";
import { initials, profileName } from "@/lib/format";
import { cn } from "@/lib/utils";

type Tile = { key: string; name: string; profile?: PublicProfile; track?: MediaStreamTrack; screen?: boolean; local?: boolean; muted?: boolean; cameraOff?: boolean; speaking?: boolean };

function MediaTile({ tile, small = false, audio = false, children }: { tile: Tile; small?: boolean; audio?: boolean; children?: ReactNode }) {
  const available = useVideoAvailable(tile.track);
  return (
    <div className="relative flex min-h-0 min-w-0 flex-col overflow-hidden rounded-[22px] border border-border bg-[radial-gradient(ellipse_at_58%_24%,var(--color-accent)_0%,var(--color-card)_65%)]">
      <div className="relative flex min-h-0 flex-1 items-center justify-center overflow-hidden">
        {available && tile.track ? (
          <CallVideo track={tile.track} label={tile.local && !tile.screen ? "Local preview" : tile.screen ? `Screen of ${tile.name}` : `Video of ${tile.name}`} allowPiP={!tile.local || tile.screen === true} />
        ) : (
          <div className="flex flex-col items-center gap-1 text-center text-xs text-muted-foreground">
            <Avatar src={tile.profile?.avatarUrls?.small || tile.profile?.avatarUrl} alt="" fallback={initials(tile.name)} className={small ? "size-7 text-xs md:size-9" : "size-[76px] rounded-[25px] bg-accent text-2xl text-signal ring-8 ring-signal/10 sm:size-28 sm:rounded-[34px] sm:text-4xl"} />
            {!small && !audio ? <span className="mt-2">{tile.local || tile.cameraOff ? "Camera off" : "No video"}</span> : null}
          </div>
        )}
      </div>
      <div className="shrink-0 px-4 pb-3 text-xs font-medium [overflow-wrap:anywhere]">{tile.name}{tile.screen ? " · Screen" : ""}{tile.muted ? <span className="ml-1 text-muted-foreground">· Muted</span> : tile.speaking ? <span className="ml-1 text-signal">· Speaking</span> : null}</div>
      {children}
    </div>
  );
}

export function CallStage({ call, media, userID, profiles }: { call: Call; media: CallView["media"]; userID: string; profiles?: Map<string, PublicProfile> }) {
  const sfu = isSfuMedia(media) ? media : undefined;
  const self: Tile = { key: userID, name: "You", local: true, track: media?.cameraOff ? undefined : media?.local.getVideoTracks()[0], speaking: call.kind === "group" && !media?.muted && sfu?.room.activeSpeakers.includes(sfu.room.localParticipant) };
  const peers = call.participants.filter(p => p.userId !== userID && (call.kind !== "group" || p.status === "accepted"));
  const remotes = sfu ? [...sfu.remotes.values()] : [];
  const peerTiles = peers.map(p => {
    const profile = profiles?.get(p.userId);
    const entry = sfu && remotes.find(item => !item.screen && remoteParticipant(call, sfu, item.identity)?.userId === p.userId);
    const remote = sfu ? entry?.stream : media?.remote;
    const status = call.kind === "group" && entry ? sfu?.remoteStatus(entry.identity) : undefined;
    return { key: p.userId, name: profileName(profile) || "Participant", profile, ...status, track: status?.cameraOff ? undefined : remote?.getVideoTracks()[0] };
  });
  const shared: Tile[] = [];
  if (media?.screenTrack?.readyState === "live") shared.push({ key: "local-share", name: "You", local: true, screen: true, track: media.screenTrack });
  for (const entry of remotes) {
    if (!entry.screen) continue;
    const participant = sfu && remoteParticipant(call, sfu, entry.identity);
    const track = entry.stream.getVideoTracks()[0];
    if (participant && track) shared.push({ key: `${participant.userId}-share`, name: profileName(profiles?.get(participant.userId)) || "Participant", screen: true, track });
  }

  if (shared.length) return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2 md:flex-row">
      <div className="grid min-h-0 flex-1 auto-rows-fr gap-2">{shared.map(tile => <MediaTile key={tile.key} tile={tile} />)}</div>
      <div className="grid max-h-32 shrink-0 auto-rows-[4.5rem] grid-cols-3 gap-2 overflow-y-auto md:max-h-full md:w-32 md:grid-cols-1">
        {[...peerTiles, self].map(tile => <MediaTile key={tile.key} tile={tile} small />)}
      </div>
    </div>
  );

  if (call.kind !== "group") return (
    <div className="grid min-h-0 min-w-0 flex-1">
      <MediaTile tile={peerTiles[0] ?? { key: "peer", name: "Participant" }} audio={call.mode === CallMode.AUDIO}>
        {media?.local.getVideoTracks().length ? (
          <div className="absolute right-2 bottom-8 grid h-16 w-20 md:h-24 md:w-32"><MediaTile tile={self} small /></div>
        ) : null}
      </MediaTile>
    </div>
  );

  const tiles = [...peerTiles, self];
  return (
    <div className={cn("grid min-h-0 min-w-0 flex-1 auto-rows-fr grid-cols-2 gap-2", tiles.length > 2 && "md:grid-cols-3")}>
      {tiles.map(tile => <MediaTile key={tile.key} tile={tile} small={tiles.length > 3} />)}
    </div>
  );
}
