"use client";

import { useRef, useState, type PointerEvent } from "react";
import { LEFT, useSwipeable, type SwipeEventData, type SwipeableHandlers } from "react-swipeable";

export const maxShiftPx = 72;
export const replyAtPx = 56;
const startAfterPx = 12;

export interface SwipeToReply {
  handlers: SwipeableHandlers;
  shift: number;
}

function selectionInside(event: SwipeEventData["event"]): boolean {
  const row = event.currentTarget;
  const selection = window.getSelection();
  return row instanceof Node && selection !== null && !selection.isCollapsed && row.contains(selection.anchorNode);
}

export function useSwipeToReply(onReply: () => void, enabled: boolean): SwipeToReply {
  const [shift, setShift] = useState(0);
  const tracking = useRef(false);

  const handlers = useSwipeable({
    delta: startAfterPx,
    trackMouse: false,
    onSwiping: ({ first, dir, deltaX, event }) => {
      if (first) tracking.current = enabled && dir === LEFT && !selectionInside(event);
      if (!tracking.current) return;
      setShift(Math.min(Math.max(-deltaX, 0), maxShiftPx));
    },
    onSwiped: ({ deltaX }) => {
      const reply = tracking.current && -deltaX >= replyAtPx;
      tracking.current = false;
      setShift(0);
      if (reply) onReply();
    },
  });

  return { handlers, shift };
}

const longPressMs = 500;
const longPressSlopPx = 10;

export function useLongPress(onLongPress: () => void, enabled: boolean) {
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const origin = useRef({ x: 0, y: 0 });

  const cancel = () => {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
  };

  return {
    onPointerDown: (event: PointerEvent<HTMLElement>) => {
      if (!enabled || event.pointerType === "mouse") return;
      origin.current = { x: event.clientX, y: event.clientY };
      cancel();
      timer.current = setTimeout(() => {
        timer.current = null;
        onLongPress();
      }, longPressMs);
    },
    onPointerMove: (event: PointerEvent<HTMLElement>) => {
      if (timer.current === null) return;
      if (Math.hypot(event.clientX - origin.current.x, event.clientY - origin.current.y) > longPressSlopPx) cancel();
    },
    onPointerUp: cancel,
    onPointerCancel: cancel,
  };
}
