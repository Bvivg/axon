"use client";

import { createContext, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";

import type { CallView } from "@/lib/calls/manager";
import { isSfuMedia, remoteParticipant } from "@/lib/calls/media-kind";

const AudioOutput = createContext<{
  needsGesture: boolean;
  play: () => void;
  outputID: string;
  setOutput: (id: string) => Promise<void>;
  error: string;
} | null>(null);

export function useCallAudio() {
  const output = useContext(AudioOutput);
  if (!output) throw new Error("Call audio requires a signed-in session");
  return output;
}

export function CallAudioRenderer({ call, media, enabled, children }: { call: CallView["call"]; media: CallView["media"]; enabled: boolean; children: ReactNode }) {
  const output = useRef<HTMLDivElement>(null);
  const [sink, setSink] = useState("");
  const [blocked, setBlocked] = useState<ReadonlySet<MediaStreamTrack>>(() => new Set());
  const [error, setError] = useState("");
  useEffect(() => {
    if (!sink) return;
    const devices = navigator.mediaDevices;
    const changed = async () => {
      const list = await devices.enumerateDevices();
      if (!list.some(device => device.kind === "audiooutput" && device.deviceId === sink)) setSink("");
    };
    const check = () => { void changed().catch(() => undefined); };
    devices?.addEventListener("devicechange", check);
    return () => devices?.removeEventListener("devicechange", check);
  }, [sink]);
  const candidates = isSfuMedia(media)
    ? [...media.remotes.values()].flatMap(remote => call && remoteParticipant(call, media, remote.identity) ? remote.stream.getAudioTracks() : [])
    : media?.remote.getAudioTracks() ?? [];
  const tracks = [...new Map(candidates.filter(track => track.readyState === "live").map(track => [track.id, track])).values()];

  function mark(track: MediaStreamTrack, needsGesture: boolean) {
    setBlocked(current => {
      if (current.has(track) === needsGesture) return current;
      const next = new Set(current);
      if (needsGesture) next.add(track); else next.delete(track);
      return next;
    });
  }

  function play() {
    if (!enabled) return;
    for (const audio of output.current?.querySelectorAll("audio") ?? []) {
      const track = (audio.srcObject as MediaStream | null)?.getAudioTracks()[0];
      if (track) void audio.play().then(() => mark(track, false)).catch(() => mark(track, true));
    }
  }

  async function setOutput(id: string) {
    setError("");
    const elements = [...(output.current?.querySelectorAll("audio") ?? [])];
    if (!elements.length) throw new Error("Audio output unavailable.");
    try {
      await Promise.all(elements.map(element => element.setSinkId(id)));
      setSink(id);
    } catch (reason) {
      await Promise.allSettled(elements.map(element => element.setSinkId(sink)));
      throw reason;
    }
  }

  return (
    <AudioOutput value={{ needsGesture: enabled && tracks.some(track => blocked.has(track)), play, outputID: sink, setOutput, error }}>
      <div ref={output} hidden data-call-audio-renderer>
        {tracks.map(track => <CallAudioTrack key={track.id} track={track} enabled={enabled} sink={sink} mark={mark} onError={setError} />)}
      </div>
      {children}
    </AudioOutput>
  );
}

function CallAudioTrack({ track, enabled, sink, mark, onError }: {
  track: MediaStreamTrack;
  enabled: boolean;
  sink: string;
  mark: (track: MediaStreamTrack, needsGesture: boolean) => void;
  onError: (error: string) => void;
}) {
  const element = useRef<HTMLAudioElement>(null);
  const report = useRef(mark);
  useLayoutEffect(() => { report.current = mark; }, [mark]);
  useLayoutEffect(() => {
    const audio = element.current;
    if (!audio) return;
    audio.srcObject = new MediaStream([track]);
    return () => {
      audio.muted = true;
      audio.pause();
      audio.srcObject = null;
      report.current(track, false);
    };
  }, [track]);
  useLayoutEffect(() => {
    const audio = element.current;
    if (!audio) return;
    audio.muted = !enabled;
    if (!enabled) return;
    let current = true;
    void audio.play().then(() => { if (current) report.current(track, false); }).catch(() => { if (current) report.current(track, true); });
    return () => { current = false; };
  }, [enabled, track]);
  useLayoutEffect(() => {
    const audio = element.current;
    if (audio?.setSinkId && audio.sinkId !== sink) void audio.setSinkId(sink).catch(() => onError("Audio output unavailable."));
  }, [sink, onError]);
  return <audio ref={element} muted={!enabled} data-call-output="1" />;
}
