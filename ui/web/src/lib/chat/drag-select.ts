"use client";

import { useEffect, useRef, type RefObject } from "react";

import { selectableBetween, selectWhole, type MessageSelection } from "@/lib/chat/selection";
import type { WireMessage } from "@/lib/ws/protocol";

const edgeZonePx = 48;
const maxScrollStepPx = 16;
const ownDrag =
  "input, textarea, a, video, audio, [contenteditable='true'], [data-no-drag-select], button:not([role='checkbox'])";

function swallowNextClick(): void {
  const swallow = (event: MouseEvent) => {
    event.preventDefault();
    event.stopPropagation();
  };
  window.addEventListener("click", swallow, { capture: true, once: true });
  setTimeout(() => window.removeEventListener("click", swallow, { capture: true }));
}

interface Drag {
  pointerId: number;
  base: MessageSelection | null;
  fromSeq: number | null;
  toSeq: number | null;
  selecting: boolean;
  cancelled: boolean;
  x: number;
  y: number;
}

export function useDragSelect({
  viewport,
  enabled,
  messages,
  selection,
  onSelect,
}: {
  viewport: RefObject<HTMLElement | null>;
  enabled: boolean;
  messages: readonly WireMessage[];
  selection: MessageSelection | null;
  onSelect: (selection: MessageSelection | null) => void;
}): void {
  const latest = useRef({ messages, selection, onSelect });
  useEffect(() => {
    latest.current = { messages, selection, onSelect };
  });

  useEffect(() => {
    const frame = viewport.current;
    if (!enabled || !frame) return;
    let drag: Drag | null = null;
    let scrollFrame = 0;

    const rowSeqAt = (x: number, y: number): number | null => {
      const box = frame.getBoundingClientRect();
      const pointX = Math.min(Math.max(x, box.left + 1), box.left + frame.clientWidth - 1);
      const pointY = Math.min(Math.max(y, box.top + 1), box.bottom - 1);
      const row = document.elementFromPoint(pointX, pointY)?.closest<HTMLElement>("[data-seq]");
      return row && frame.contains(row) ? Number(row.dataset.seq) : null;
    };

    const update = () => {
      if (!drag || drag.cancelled) return;
      const seq = rowSeqAt(drag.x, drag.y);
      if (seq === null) return;
      if (drag.fromSeq === null) {
        drag.fromSeq = seq;
        drag.toSeq = seq;
        return;
      }
      if (!drag.selecting) {
        if (seq === drag.fromSeq) return;
        drag.selecting = true;
        frame.style.userSelect = "none";
      }
      window.getSelection()?.removeAllRanges();
      if (seq === drag.toSeq) return;
      drag.toSeq = seq;
      const { messages: current, onSelect: select } = latest.current;
      select(selectWhole(drag.base, selectableBetween(current, drag.fromSeq, seq)));
    };

    const edgeStep = (): number => {
      if (!drag?.selecting || drag.cancelled) return 0;
      const box = frame.getBoundingClientRect();
      const speed = (distance: number) =>
        Math.ceil(maxScrollStepPx * Math.min(1, (edgeZonePx - distance) / edgeZonePx));
      if (drag.y - box.top < edgeZonePx) return -speed(drag.y - box.top);
      if (box.bottom - drag.y < edgeZonePx) return speed(box.bottom - drag.y);
      return 0;
    };

    const autoScroll = () => {
      scrollFrame = 0;
      const step = edgeStep();
      if (step === 0) return;
      frame.scrollTop += step;
      update();
      scrollFrame = requestAnimationFrame(autoScroll);
    };

    const onMove = (event: PointerEvent) => {
      if (!drag || event.pointerId !== drag.pointerId) return;
      drag.x = event.clientX;
      drag.y = event.clientY;
      update();
      if (scrollFrame === 0 && edgeStep() !== 0) scrollFrame = requestAnimationFrame(autoScroll);
    };

    const onScroll = () => update();

    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape" && drag) drag.cancelled = true;
    };

    const stop = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onEnd);
      window.removeEventListener("pointercancel", onEnd);
      window.removeEventListener("keydown", onKey);
      frame.removeEventListener("scroll", onScroll);
      cancelAnimationFrame(scrollFrame);
      scrollFrame = 0;
      frame.style.userSelect = "";
      drag = null;
    };

    function onEnd(event: PointerEvent) {
      if (!drag || event.pointerId !== drag.pointerId) return;
      if (drag.selecting && event.type === "pointerup") swallowNextClick();
      stop();
    }

    const onDown = (event: PointerEvent) => {
      if (event.pointerType !== "mouse" || event.button !== 0) return;
      if (event.target === frame) return;
      if (event.target instanceof Element && event.target.closest(ownDrag)) return;
      stop();
      const fromSeq = rowSeqAt(event.clientX, event.clientY);
      drag = {
        pointerId: event.pointerId,
        base: latest.current.selection,
        fromSeq,
        toSeq: fromSeq,
        selecting: false,
        cancelled: false,
        x: event.clientX,
        y: event.clientY,
      };
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onEnd);
      window.addEventListener("pointercancel", onEnd);
      window.addEventListener("keydown", onKey);
      frame.addEventListener("scroll", onScroll, { passive: true });
    };

    frame.addEventListener("pointerdown", onDown);
    return () => {
      frame.removeEventListener("pointerdown", onDown);
      stop();
    };
  }, [viewport, enabled]);
}
