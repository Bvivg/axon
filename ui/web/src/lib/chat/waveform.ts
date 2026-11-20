export const waveformBarCount = 48;

export function waveformPeaks(channels: readonly Float32Array[], count = waveformBarCount): number[] {
  if (count <= 0) return [];
  const length = channels.length ? Math.min(...channels.map(channel => channel.length)) : 0;
  const peaks = Array<number>(count).fill(0);
  if (!length) return peaks;
  for (let i = 0; i < count; i++) {
    const start = Math.min(length - 1, Math.floor(i * length / count));
    const end = Math.min(length, Math.max(start + 1, Math.floor((i + 1) * length / count)));
    let power = 0;
    for (const channel of channels) {
      for (let sample = start; sample < end; sample++) power += channel[sample] ** 2;
    }
    peaks[i] = Math.sqrt(power / ((end - start) * channels.length));
  }
  const maximum = Math.max(...peaks);
  return maximum > 0 ? peaks.map(peak => peak / maximum) : peaks;
}
