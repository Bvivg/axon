import { expect, test } from "@playwright/test";

import { gatewayUrl, newUsername, password } from "./support";

test("media cookie authenticates only GET and HEAD of private media", async ({ request, page, baseURL }) => {
  const origin = new URL(baseURL!).origin;
  const rpcHeaders = { Origin: origin, "Connect-Protocol-Version": "1" };
  const auth = `${gatewayUrl}/axon.auth.v1.AuthService`;
  const username = newUsername("media-cookie");
  const registration = await request.post(`${auth}/Register`, {
    headers: rpcHeaders,
    data: { username, email: `${username}@example.test`, password, displayName: "Media Cookie" },
  });
  expect(registration.status()).toBe(200);
  const cookies = (await request.storageState()).cookies;
  const media = cookies.find(cookie => cookie.name === "axon_media");
  expect(media?.path).toBe("/api/media");
  expect(media?.sameSite).toBe("Lax");
  const cookie = { Cookie: `axon_media=${media!.value}` };
  const { tokens } = await registration.json();
  const bearer = { Authorization: `Bearer ${tokens.accessToken}` };
  let conditional: { url: string; etag: string } | undefined;
  try {
    expect((await request.post(`${auth}/GetMe`, { headers: { ...rpcHeaders, ...cookie }, data: {} })).status()).toBe(401);
    for (const route of ["/ws/chat", "/ws/presence", "/ws/game"]) {
      expect((await request.get(gatewayUrl + route, { headers: cookie })).status(), route).toBe(401);
    }
    for (const route of ["/api/avatar", "/api/attachment", "/api/uploads"]) {
      expect((await request.post(gatewayUrl + route, { headers: cookie, data: "test" })).status(), route).toBe(401);
    }
    expect((await request.put(`${gatewayUrl}/api/uploads/test/parts/1`, { headers: cookie, data: "test" })).status()).toBe(401);

    const png = await page.evaluate(() => document.createElement("canvas").toDataURL().split(",")[1]);
    const uploaded = await request.post(`${gatewayUrl}/api/attachment?as=media`, {
      headers: { ...bearer, "Content-Type": "image/png", "X-Filename": "media-cookie.png" },
      data: Buffer.from(png, "base64"),
    });
    expect(uploaded.status()).toBe(200);
    const { payload } = await uploaded.json();
    const key = new URL(payload.url).pathname.split("/chat-attachments/")[1];
    const mediaURL = new URL(`/api/media/${key}`, gatewayUrl).href;
    if (new URL(gatewayUrl).pathname === "/api") {
      const oldURL = `${gatewayUrl}/api/media/${key}`;
      const redirect = await request.get(oldURL, { maxRedirects: 0 });
      expect(redirect.status()).toBe(308);
      expect(redirect.headers().location).toBe(new URL(mediaURL).pathname);
      expect((await request.get(oldURL)).status()).toBe(200);
    }
    for (const method of ["GET", "HEAD"]) {
      expect((await request.fetch(mediaURL, { method, headers: cookie })).status(), method).toBe(200);
      expect((await request.fetch(mediaURL, { method, headers: { ...cookie, Origin: origin } })).status(), method).toBe(200);
      expect((await request.fetch(mediaURL, { method, headers: { ...cookie, Origin: "https://untrusted.example" } })).status(), method).toBe(403);
    }
    expect((await request.post(mediaURL, { headers: cookie })).status()).toBe(405);
    const download = await request.get(mediaURL, { headers: cookie });
    const etag = download.headers().etag;
    expect(etag).toBeTruthy();
    expect(download.headers()["last-modified"]).toBeTruthy();
    const cacheDirectives = download.headers()["cache-control"].split(",").map(value => value.trim());
    expect(new Set(cacheDirectives)).toEqual(new Set(["private", "no-store"]));
    expect((await request.get(mediaURL, { headers: { ...cookie, "If-None-Match": etag } })).status()).toBe(304);
    conditional = { url: mediaURL, etag };
  } finally {
    expect((await request.post(`${auth}/Logout`, { headers: { ...rpcHeaders, ...bearer }, data: {} })).status()).toBe(200);
  }
  expect((await request.storageState()).cookies.some(cookie => cookie.name === "axon_media")).toBe(false);
  for (const method of ["GET", "HEAD"]) {
    expect((await request.fetch(conditional!.url, {
      method,
      headers: { ...cookie, "If-None-Match": conditional!.etag },
    })).status(), method).toBe(401);
  }
});
