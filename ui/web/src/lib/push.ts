import { chatClient } from "@/lib/connect/clients";

const workerUrl = "/sw.js";

export function pushSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    window.isSecureContext &&
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window
  );
}

export async function currentSubscription(): Promise<PushSubscription | null> {
  if (!pushSupported()) return null;
  const registration = await navigator.serviceWorker.getRegistration("/");
  return (await registration?.pushManager.getSubscription()) ?? null;
}

export async function enablePush(publicKey: string): Promise<void> {
  if ((await Notification.requestPermission()) !== "granted") {
    throw new Error("Notifications are blocked for this site. Allow them in the browser's site settings.");
  }
  await register(await subscribe(publicKey));
}

export async function disablePush(): Promise<void> {
  const subscription = await currentSubscription();
  if (!subscription) return;
  try {
    await chatClient.unsubscribePush({ endpoint: subscription.endpoint });
  } finally {
    await subscription.unsubscribe();
  }
}

export async function resyncPush(publicKey: string): Promise<void> {
  if (!publicKey || Notification.permission !== "granted" || !(await currentSubscription())) return;
  await register(await subscribe(publicKey));
}

export function listenForOpenRequests(open: (url: string) => void): () => void {
  if (!pushSupported()) return () => undefined;
  const onMessage = (event: MessageEvent) => {
    const data = event.data as { type?: string; url?: string } | null;
    if (data?.type === "open" && typeof data.url === "string" && data.url.startsWith("/")) {
      open(data.url);
    }
  };
  navigator.serviceWorker.addEventListener("message", onMessage);
  navigator.serviceWorker.startMessages();
  return () => navigator.serviceWorker.removeEventListener("message", onMessage);
}

async function subscribe(publicKey: string): Promise<PushSubscription> {
  await navigator.serviceWorker.register(workerUrl, { scope: "/" });
  const registration = await navigator.serviceWorker.ready;
  const key = decode(publicKey);
  const existing = await registration.pushManager.getSubscription();
  if (existing && sameKey(existing.options.applicationServerKey, key)) {
    return existing;
  }
  await existing?.unsubscribe();
  return registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
}

async function register(subscription: PushSubscription): Promise<void> {
  const keys = subscription.toJSON().keys ?? {};
  await chatClient.subscribePush({
    endpoint: subscription.endpoint,
    p256dh: keys.p256dh ?? "",
    auth: keys.auth ?? "",
  });
}

function decode(base64url: string): Uint8Array<ArrayBuffer> {
  const base64 = (base64url + "=".repeat((4 - (base64url.length % 4)) % 4)).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  const bytes = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i += 1) bytes[i] = raw.charCodeAt(i);
  return bytes;
}

function sameKey(current: ArrayBuffer | null, wanted: Uint8Array): boolean {
  if (!current || current.byteLength !== wanted.byteLength) return false;
  const bytes = new Uint8Array(current);
  return bytes.every((byte, i) => byte === wanted[i]);
}
