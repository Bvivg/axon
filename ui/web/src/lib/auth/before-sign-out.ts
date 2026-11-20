const handlers = new Set<() => Promise<void>>();

export function onBeforeSignOut(handler: () => Promise<void>) {
  handlers.add(handler);
  return () => { handlers.delete(handler); };
}

export async function beforeSignOut() {
  await Promise.allSettled([...handlers].map(handler => handler()));
}
