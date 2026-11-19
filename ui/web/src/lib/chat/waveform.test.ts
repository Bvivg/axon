import { describe, expect, it } from "vitest";

import { waveformPeaks } from "@/lib/chat/waveform";

describe("voice waveform", () => {
  it("preserves silent, loud and quiet sections of the recording", () => {
    expect(waveformPeaks([Float32Array.from([0, 0, 1, 1, 0, 0, .5, .5])], 4)).toEqual([0, 1, 0, .5]);
  });

  it("keeps stereo audio visible when channels have opposite phases", () => {
    expect(waveformPeaks([Float32Array.from([1, .5]), Float32Array.from([-1, -.5])], 2)).toEqual([1, .5]);
  });

  it("handles silence and empty recordings without invalid amplitudes", () => {
    expect(waveformPeaks([new Float32Array(100)], 4)).toEqual([0, 0, 0, 0]);
    expect(waveformPeaks([], 4)).toEqual([0, 0, 0, 0]);
  });

  it("fills the waveform even when there are fewer samples than bars", () => {
    expect(waveformPeaks([Float32Array.from([0, 1])], 4)).toEqual([0, 0, 1, 1]);
  });
});
