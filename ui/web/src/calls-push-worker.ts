/// <reference lib="webworker" />

import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AuthService } from "./gen/axon/auth/v1/auth_pb";
import { CallsService, CallState } from "./gen/axon/calls/v1/calls_pb";

const worker = self as unknown as ServiceWorkerGlobalScope;
const transport = createConnectTransport({ baseUrl: new URL(process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:18080", worker.location.origin).href, fetch: (input, init) => fetch(input, { ...init, credentials: "include" }) });
const auth = createClient(AuthService, transport);
const calls = createClient(CallsService, transport);

worker.addEventListener("install", event => { event.waitUntil(worker.skipWaiting()); });
worker.addEventListener("activate", event => { event.waitUntil(worker.clients.claim()); });
worker.addEventListener("push", event => {
  event.waitUntil((async () => {
    let data: { type?: string; call_id?: string; deadline?: string; title?: string; body?: string; tag?: string; url?: string };
    try { data = event.data?.json() ?? {}; } catch { return; }
    if (data.type === "call") {
      if (!data.call_id || !data.deadline || Date.parse(data.deadline) <= Date.now()) return;
      try {
        const session = await auth.refreshToken({}, { timeoutMs: 3000 });
        if (!session.tokens?.accessToken) return;
        const { call } = await calls.getCall({ callId: data.call_id }, { headers: { Authorization: `Bearer ${session.tokens.accessToken}` }, timeoutMs: 3000 });
        if (!call || call.state !== CallState.RINGING || !call.deadline || Number(call.deadline.seconds) * 1000 <= Date.now()) return;
      } catch { return; }
    }
    await worker.registration.showNotification(data.title || "Axon", { body: data.body || "", tag: data.tag, data: { url: data.url || "/chat", callId: data.call_id } });
  })());
});
worker.addEventListener("message", event => {
  if (event.data?.type === "call-ended" && typeof event.data.callId === "string") event.waitUntil((async () => {
    for (const notification of await worker.registration.getNotifications({ tag: `call-${event.data.callId}` })) notification.close();
  })());
});
worker.addEventListener("notificationclick", event => {
  event.notification.close();
  const url = new URL(event.notification.data?.url || "/calls", worker.location.origin);
  if (url.origin !== worker.location.origin) return;
  event.waitUntil((async () => {
    const windows = await worker.clients.matchAll({ type: "window", includeUncontrolled: true });
    const open = windows.find(client => new URL(client.url).origin === url.origin);
    if (!open) { await worker.clients.openWindow(url.href); return; }
    await open.focus(); open.postMessage({ type: "open", url: url.pathname + url.search + url.hash });
  })());
});
