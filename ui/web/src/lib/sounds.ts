type Note = { frequency: number; start: number; duration: number };

const cues = {
  incoming: [{ frequency: 660, start: 0, duration: 0.19 }, { frequency: 880, start: 0.25, duration: 0.24 }],
  outgoing: [{ frequency: 440, start: 0, duration: 0.65 }, { frequency: 480, start: 0, duration: 0.65 }],
  connected: [{ frequency: 523, start: 0, duration: 0.11 }, { frequency: 784, start: 0.1, duration: 0.17 }],
  ended: [{ frequency: 523, start: 0, duration: 0.11 }, { frequency: 349, start: 0.1, duration: 0.18 }],
  message: [{ frequency: 988, start: 0, duration: 0.17 }],
} satisfies Record<string, Note[]>;

type Ring = "incoming" | "outgoing";
type Cue = keyof typeof cues;

export class AppSounds {
  private audio?: AudioContext;
  private ringing?: Ring;
  private interval?: ReturnType<typeof setInterval>;
  private callTones = new Set<OscillatorNode>();
  private messageTones = new Set<OscillatorNode>();
  private lastMessageAt = -Infinity;

  unlock = () => {
    try {
      if (!this.audio) {
        this.audio = new AudioContext();
        this.audio.addEventListener("statechange", this.syncRing);
      }
      if (this.audio.state !== "running") void this.audio.resume().catch(() => undefined);
      else this.syncRing();
    } catch {}
  };

  syncRing = () => {
    if (!this.ready()) {
      this.stopRing();
      this.stop(this.messageTones);
      return;
    }
    const ring = this.ringing;
    if (!ring || this.interval !== undefined) return;
    this.stop(this.messageTones);
    this.play(ring, this.callTones);
    this.interval = setInterval(() => this.play(ring, this.callTones), ring === "incoming" ? 2400 : 3000);
  };

  ring(cue?: Ring) {
    if (this.ringing === cue) return;
    this.stopRing();
    this.ringing = cue;
    this.syncRing();
  }

  call(cue: "connected" | "ended") {
    this.stop(this.messageTones);
    this.stop(this.callTones);
    this.play(cue, this.callTones);
  }

  message() {
    const now = performance.now();
    if (!this.ready() || this.ringing || this.callTones.size || now - this.lastMessageAt < 600) return;
    this.lastMessageAt = now;
    this.play("message", this.messageTones);
  }

  muteMessages() { this.stop(this.messageTones); }
  muteCalls() { this.ringing = undefined; this.stopRing(); }

  close() {
    this.muteCalls();
    this.muteMessages();
    this.audio?.removeEventListener("statechange", this.syncRing);
    void this.audio?.close().catch(() => undefined);
    this.audio = undefined;
  }

  private ready() {
    return this.audio?.state === "running" && document.visibilityState === "visible";
  }

  private stopRing() {
    clearInterval(this.interval);
    this.interval = undefined;
    this.stop(this.callTones);
  }

  private stop(tones: Set<OscillatorNode>) {
    for (const tone of tones) {
      tone.stop();
      tone.disconnect();
    }
    tones.clear();
  }

  private play(cue: Cue, tones: Set<OscillatorNode>) {
    const audio = this.audio;
    if (!audio || !this.ready()) return;
    const now = audio.currentTime;
    for (const note of cues[cue]) {
      const start = now + note.start;
      const end = start + note.duration;
      const tone = audio.createOscillator();
      const gain = audio.createGain();
      tone.frequency.value = note.frequency;
      gain.gain.setValueAtTime(0, start);
      gain.gain.linearRampToValueAtTime(cue === "outgoing" ? 0.045 : 0.085, start + 0.025);
      gain.gain.linearRampToValueAtTime(0, end);
      tone.connect(gain).connect(audio.destination);
      tones.add(tone);
      tone.onended = () => { tones.delete(tone); tone.disconnect(); gain.disconnect(); };
      tone.start(start);
      tone.stop(end);
    }
  }
}
