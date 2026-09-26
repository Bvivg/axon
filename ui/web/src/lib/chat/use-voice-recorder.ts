"use client";

import { useCallback, useEffect, useRef, useState } from "react";

export enum RecorderState {
  Idle = "idle",
  Starting = "starting",
  Recording = "recording",
}

export interface Recording {
  blob: Blob;
  durationMs: number;
  filename: string;
}

const preferredTypes = ["audio/webm;codecs=opus", "audio/mp4", "audio/ogg;codecs=opus", "audio/webm"];

const maxDurationMs = 15 * 60 * 1000;

function pickType(): string | undefined {
  if (typeof MediaRecorder === "undefined") {
    return undefined;
  }
  return preferredTypes.find((type) => MediaRecorder.isTypeSupported(type));
}

function extensionFor(type: string): string {
  if (type.startsWith("audio/mp4")) return "m4a";
  if (type.startsWith("audio/ogg")) return "ogg";
  return "webm";
}

export function useVoiceRecorder() {
  const [state, setState] = useState(RecorderState.Idle);
  const [elapsedMs, setElapsedMs] = useState(0);
  const [error, setError] = useState<string | null>(null);

  const recorder = useRef<MediaRecorder | null>(null);
  const stream = useRef<MediaStream | null>(null);
  const chunks = useRef<Blob[]>([]);
  const startedAt = useRef(0);
  const finish = useRef<((recording: Recording | null) => void) | null>(null);

  const release = useCallback(() => {
    stream.current?.getTracks().forEach((track) => track.stop());
    stream.current = null;
    recorder.current = null;
    chunks.current = [];
    setState(RecorderState.Idle);
    setElapsedMs(0);
  }, []);

  useEffect(() => {
    if (state !== RecorderState.Recording) {
      return;
    }
    const timer = setInterval(() => {
      const elapsed = Date.now() - startedAt.current;
      setElapsedMs(elapsed);
      if (elapsed >= maxDurationMs) {
        recorder.current?.stop();
      }
    }, 200);
    return () => clearInterval(timer);
  }, [state]);

  useEffect(() => () => {
    finish.current = null;
    recorder.current?.stop();
    stream.current?.getTracks().forEach((track) => track.stop());
  }, []);

  const start = useCallback(async () => {
    if (state !== RecorderState.Idle) {
      return;
    }
    setError(null);

    const type = pickType();
    if (!navigator.mediaDevices?.getUserMedia || type === undefined) {
      setError("This browser cannot record audio.");
      return;
    }

    setState(RecorderState.Starting);
    try {
      stream.current = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch {
      setError("Allow microphone access to record a voice message.");
      release();
      return;
    }

    const next = new MediaRecorder(stream.current, { mimeType: type });
    chunks.current = [];
    next.ondataavailable = (event) => {
      if (event.data.size > 0) {
        chunks.current.push(event.data);
      }
    };
    next.onstop = () => {
      const durationMs = Date.now() - startedAt.current;
      const blob = new Blob(chunks.current, { type: next.mimeType || type });
      const done = finish.current;
      finish.current = null;
      release();
      done?.(blob.size > 0 ? { blob, durationMs, filename: `voice.${extensionFor(blob.type)}` } : null);
    };

    recorder.current = next;
    startedAt.current = Date.now();
    next.start(250);
    setState(RecorderState.Recording);
  }, [state, release]);

  const stop = useCallback(
    () =>
      new Promise<Recording | null>((resolve) => {
        const current = recorder.current;
        if (!current || current.state === "inactive") {
          resolve(null);
          return;
        }
        finish.current = resolve;
        current.stop();
      }),
    [],
  );

  const cancel = useCallback(() => {
    finish.current = null;
    const current = recorder.current;
    if (current && current.state !== "inactive") {
      current.stop();
    } else {
      release();
    }
  }, [release]);

  return { state, elapsedMs, error, start, stop, cancel };
}
