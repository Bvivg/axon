self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("push", (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch {
    data = { body: event.data ? event.data.text() : "" };
  }
  event.waitUntil(
    self.registration.showNotification(data.title || "Axon", {
      body: data.body || "",
      tag: data.tag || undefined,
      renotify: Boolean(data.tag),
      data: { url: data.url || "/chat" },
    }),
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = new URL(event.notification.data?.url || "/chat", self.location.origin);
  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
      const open = windows.find((client) => new URL(client.url).origin === url.origin);
      if (!open) {
        await self.clients.openWindow(url.href);
        return;
      }
      await open.focus();
      open.postMessage({ type: "open", url: url.pathname + url.search + url.hash });
    })(),
  );
});
