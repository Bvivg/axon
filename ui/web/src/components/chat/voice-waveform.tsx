"use client";

import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState, type ChangeEventHandler } from "react";

import { waveformBarCount, waveformPeaks } from "@/lib/chat/waveform";
import { formatDuration } from "@/lib/format";
import { fetchWithRateLimitRetry } from "@/lib/rate-limit";

const unloadedPeaks = Array<number>(waveformBarCount).fill(0);

export function VoiceWaveform({ url, durationMs, positionMs, onSeek }: {
  url: string;
  durationMs: number;
  positionMs: number;
  onSeek: ChangeEventHandler<HTMLInputElement>;
}) {
  const container = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    const element = container.current;
    if (!element) return;
    const observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) {
        setVisible(true);
        observer.disconnect();
      }
    }, { rootMargin: "100px" });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const waveform = useQuery({
    queryKey: ["voice-waveform", url],
    enabled: visible && !!url,
    queryFn: async ({ signal }) => {
      const response = await fetchWithRateLimitRetry(url, { credentials: "include", signal });
      if (!response.ok) throw new Error("Could not load the waveform.");
      const decoder = new OfflineAudioContext(1, 1, 8000);
      const audio = await decoder.decodeAudioData(await response.arrayBuffer());
      return waveformPeaks(Array.from({ length: audio.numberOfChannels }, (_, i) => audio.getChannelData(i)));
    },
    staleTime: Infinity,
    gcTime: 5 * 60_000,
    retry: false,
  });
  const peaks = waveform.data ?? unloadedPeaks;
  const progress = durationMs > 0 ? Math.min(1, positionMs / durationMs) : 0;

  return (
    <div ref={container} className="voice-waveform" data-waveform-ready={waveform.isSuccess}>
      <div aria-hidden className="voice-waveform-bars">
        {peaks.map((peak, i) => <span key={i} className="voice-waveform-bar" data-played={progress > i / peaks.length} style={{ height: `${3 + 21 * Math.sqrt(peak)}px` }} />)}
      </div>
      <input type="range" min={0} max={Math.max(durationMs, 1)} step={100} value={Math.min(positionMs, durationMs)} onChange={onSeek} aria-label="Voice message position" aria-valuetext={`${formatDuration(positionMs)} of ${formatDuration(durationMs)}`} />
    </div>
  );
}
