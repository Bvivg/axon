const easing = "cubic-bezier(.2, .75, .25, 1)";

export function reducedMotion(): boolean {
  return typeof window === "undefined" || window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export function slideThrough(element: Element | null | undefined, squares: (Element | null | undefined)[]): void {
  if (!element || squares.length < 2 || squares.some(square => !square) || reducedMotion()) return;
  if (!(element instanceof HTMLElement || element instanceof SVGElement)) return;
  const end = squares.at(-1)!.getBoundingClientRect();
  const frames = squares.map(square => {
    const rect = square!.getBoundingClientRect();
    return { transform: `translate(${rect.left - end.left}px, ${rect.top - end.top}px)` };
  });
  if (frames.every(frame => frame.transform === "translate(0px, 0px)")) return;
  const previous = element.style.zIndex;
  element.style.zIndex = "20";
  const animation = element.animate(frames, { duration: Math.min(120 + 140 * (squares.length - 1), 640), easing });
  const restore = () => { element.style.zIndex = previous; };
  animation.onfinish = restore;
  animation.oncancel = restore;
}

type Place = Element | DOMRect | null | undefined;

function centerOf(place: Place): { x: number; y: number } | null {
  if (!place) return null;
  const rect = place instanceof Element ? place.getBoundingClientRect() : place;
  return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
}

function lift(element: HTMLElement | SVGElement, animation: Animation, onDone?: () => void): Animation {
  const previous = element.style.zIndex;
  element.style.zIndex = "30";
  const restore = () => { element.style.zIndex = previous; onDone?.(); };
  animation.onfinish = restore;
  animation.oncancel = restore;
  return animation;
}

export function flyFrom(element: Element | null | undefined, source: Place, delay = 0): void {
  if (!(element instanceof HTMLElement || element instanceof SVGElement) || reducedMotion()) return;
  const from = centerOf(source), to = centerOf(element);
  if (!from || !to) return;
  const dx = from.x - to.x, dy = from.y - to.y;
  lift(element, element.animate(
    [{ transform: `translate(${dx}px, ${dy}px) scale(.82) rotate(-6deg)`, opacity: .7 }, { transform: "none", opacity: 1 }],
    { duration: 360, delay, easing, fill: "backwards" },
  ));
}

export function flyTo(element: Element | null | undefined, target: Place, delay = 0, onDone?: () => void): boolean {
  if (!(element instanceof HTMLElement || element instanceof SVGElement) || reducedMotion()) return false;
  const from = centerOf(element), to = centerOf(target);
  if (!from || !to) return false;
  const dx = to.x - from.x, dy = to.y - from.y;
  lift(element, element.animate(
    [{ transform: "none", opacity: 1 }, { transform: `translate(${dx}px, ${dy}px) scale(.7) rotate(10deg)`, opacity: .15 }],
    { duration: 420, delay, easing: "cubic-bezier(.45, 0, .55, 1)", fill: "forwards" },
  ), onDone);
  return true;
}
