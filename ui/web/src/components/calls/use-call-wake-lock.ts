import { useEffect, useRef, useState } from "react";

export function useCallWakeLock(enabled: boolean) {
  const pending = useRef<Promise<void>>(Promise.resolve());
  const [held, setHeld] = useState(false);
  useEffect(() => {
    if (!enabled || !navigator.wakeLock) return;
    let disposed = false;
    let sentinel: WakeLockSentinel | undefined;
    const sync = () => {
      pending.current = pending.current.then(async () => {
        if (disposed) return;
        if (document.visibilityState !== "visible") {
          await sentinel?.release();
          return;
        }
        if (sentinel && !sentinel.released) return;
        try {
          const next = await navigator.wakeLock.request("screen");
          if (disposed || document.visibilityState !== "visible") {
            await next.release();
            return;
          }
          sentinel = next;
          next.addEventListener("release", () => { if (!disposed) setHeld(false); }, { once: true });
          setHeld(!next.released);
        } catch {
          if (!disposed) setHeld(false);
        }
      }).catch(() => { if (!disposed) setHeld(false); });
    };
    sync();
    document.addEventListener("visibilitychange", sync);
    return () => {
      disposed = true;
      document.removeEventListener("visibilitychange", sync);
      pending.current = pending.current.then(() => sentinel?.release()).catch(() => undefined);
    };
  }, [enabled]);
  return enabled && held;
}
