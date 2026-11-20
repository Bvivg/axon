import type { PeerMedia } from "@/lib/calls/p2p";
import type { SfuMedia } from "@/lib/calls/livekit";
import type { Call } from "@/gen/axon/calls/v1/calls_pb";

export function isSfuMedia(media?: PeerMedia | SfuMedia): media is SfuMedia {
  return !!media && "room" in media;
}

export function remoteParticipant(call: Call, media: SfuMedia, identity: string) {
  const binding = media.call.generation === call.generation ? call : media.call;
  const participant = binding.participants.find(p => identity === `${p.endpointId}:${p.endpointEpoch}:${binding.generation}:${p.mediaVersion}`);
  return participant && call.participants.find(p => p.userId === participant.userId && p.status === "accepted");
}
