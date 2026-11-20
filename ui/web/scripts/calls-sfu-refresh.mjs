import fs from "node:fs";
import { randomUUID, createHmac } from "node:crypto";
import { chromium } from "playwright";

const origin = "http://localhost:3000", gateway = "http://localhost:8080";
const migration = process.env.CALLS_REFRESH_MIGRATION === "1";
const started = Date.now(), limit = 12 * 60 * 1000;
const evidence = { startedAt: new Date(started).toISOString(), limitMs: limit, reconnect: [], denied: [] };
const save = () => fs.writeFileSync(migration ? "/evidence/migration-refresh.json" : "/evidence/sfu-refresh.json", JSON.stringify(evidence, null, 2));
const watchdog = setTimeout(() => { evidence.passed = false; evidence.error = "Declared12min real-clock budget exceeded"; save(); process.exit(1); }, limit);
async function until(fn, timeout = 10000) {
  const deadline = Math.min(started + limit, Date.now() + timeout);
  while (Date.now() < deadline) { if (await fn()) return; await new Promise(resolve => setTimeout(resolve, 100)); }
  throw new Error("Condition deadline exceeded");
}
const secrets = JSON.parse(fs.readFileSync("/evidence/.secrets/admission.json", "utf8"));
async function participants(room) {
  const head = Buffer.from(JSON.stringify({ alg: "HS256", typ: "JWT" })).toString("base64url");
  const body = head + "." + Buffer.from(JSON.stringify({ iss: secrets.key, sub: "sfu-evidence", exp: Math.floor(Date.now() / 1000) + 30, video: { roomAdmin: true, room } })).toString("base64url");
  const token = body + "." + createHmac("sha256", secrets.secret).update(body).digest("base64url");
  const response = await fetch("http://livekit:7880/twirp/livekit.RoomService/ListParticipants", { method: "POST", headers: { "content-type": "application/json", Authorization: `Bearer ${token}` }, body: JSON.stringify({ room }) });
  if (!response.ok) throw new Error(`Room API ${response.status}`);
  return (await response.json()).participants ?? [];
}
const browser = await chromium.launch({ channel: "chromium", args: ["--use-fake-device-for-media-stream", "--use-file-for-fake-audio-capture=/evidence/.secrets/tone.wav"] });
const people = [];
async function snapshot(person) {
  return person.page.evaluate(async () => {
    const media = window.axonSFURetained;
    const rows = [];
    for (const pc of media.pcs) { const report = await pc.getStats(); report.forEach(row => rows.push({ ...row })); }
    const sum = (key, kind) => rows.filter(row => row.type === "inbound-rtp" && row.kind === kind).reduce((n, row) => n + Number(row[key] || 0), 0);
    return { state: media.room.state, participants: [...media.room.remoteParticipants.values()].map(p => ({ identity: p.identity, sid: p.sid })), audioBytes: sum("bytesReceived", "audio"), energy: sum("totalAudioEnergy", "audio"), frames: sum("framesDecoded", "video"), tracks: [...media.tracks].map(t => ({ kind: t.kind, readyState: t.readyState })) };
  });
}
try {
  console.log("Production four-part identity refresh probe started; declared12min budget, E2E timeouts unchanged");
  for (let i = 0; i < (migration ? 2 : 3); i++) {
    const context = await browser.newContext({ permissions: ["microphone", "camera"] });
    const suffix = randomUUID().replaceAll("-", "").slice(0, 12);
    const registered = await context.request.post(`${gateway}/axon.auth.v1.AuthService/Register`, { headers: { Origin: origin, "Connect-Protocol-Version": "1" }, data: { email: `sfu-${suffix}@axon.test`, username: `sfu_${suffix}`, password: "correct-horse-battery-staple", displayName: `SFU ${i}` } });
    if (!registered.ok()) throw new Error(`Register ${registered.status()}`);
    const body = await registered.json(), page = await context.newPage();
    const person = { context, page, userID: body.user.id, username: `sfu_${suffix}`, email: `sfu-${suffix}@axon.test`, name: `SFU ${i}`, token: body.tokens.accessToken, sockets: [] };
    page.on("websocket", socket => { const url = new URL(socket.url()); if (!url.pathname.startsWith("/livekit/")) return; const raw = url.searchParams.get("access_token"); if (raw) { const claims = JSON.parse(Buffer.from(raw.split(".")[1], "base64url").toString()); person.sockets.push({ at: Date.now(), path: url.pathname, identity: claims.sub, exp: claims.exp, room: claims.video.room }); } });
    people.push(person);
  }
  const [a, b, c] = people;
  let chatRoom;
  if (migration) {
    await a.page.goto(`${origin}/chat`);
    await a.page.getByRole("button", { name: "New chat", exact: true }).click();
    const composer = a.page.getByRole("region", { name: "New conversation" });
    await composer.getByLabel("Search people").fill(b.email);
    await composer.getByRole("button", { name: b.name }).click();
    await a.page.getByLabel("Message", { exact: true }).fill("Migration real refresh");
    await a.page.getByRole("button", { name: "Send", exact: true }).click();
    await until(async () => {
      const response = await a.context.request.post(`${gateway}/axon.chat.v1.ChatService/GetDirectRoom`, { headers: { Origin: origin, "Connect-Protocol-Version": "1", Authorization: `Bearer ${a.token}` }, data: { userId: b.userID } });
      if (!response.ok()) throw new Error(`GetDirectRoom ${response.status()}`);
      chatRoom = (await response.json()).room?.id; return !!chatRoom;
    });
  } else {
    const created = await a.context.request.post(`${gateway}/axon.chat.v1.ChatService/CreateGroup`, { headers: { Origin: origin, "Connect-Protocol-Version": "1", Authorization: `Bearer ${a.token}` }, data: { name: "SFU real refresh", memberUserIds: [b.userID, c.userID] } });
    if (!created.ok()) throw new Error(`CreateGroup ${created.status()}`);
    chatRoom = (await created.json()).room.id;
  }
  for (const person of people) {
    await person.page.goto(person === a ? `${origin}/chat#${migration ? b.username : chatRoom}` : `${origin}/calls`);
    await until(() => person.page.evaluate(() => window.axonCallManager?.snapshot().sfuAvailable));
  }
  await a.page.getByRole("button", { name: "Video call", exact: true }).click();
  for (const person of people.slice(1)) await person.page.getByRole("button", { name: "Accept", exact: true }).click();
  if (migration) {
    for (const person of people) {
      await until(() => person.page.evaluate(() => window.axonCallManager.snapshot().call?.state === 3));
      await person.page.evaluate(() => { window.axonMigrationSource = window.axonCallManager.snapshot().media; });
    }
    evidence.original = await a.page.evaluate(() => { const c = window.axonCallManager.snapshot().call; return { id: c.id, started: String(c.startedAt.seconds) }; });
    const input = await a.page.evaluate(() => { const m = window.axonCallManager, c = m.snapshot().call, e = m.channel.endpoint; return { callId: c.id, generation: String(c.generation), endpoint: { id: e.id, epoch: String(e.epoch), proof: e.proof }, requestId: crypto.randomUUID() }; });
    const requested = await a.context.request.post(`${gateway}/axon.calls.v1.CallsService/RequestCallMigration`, { headers: { Origin: origin, "Connect-Protocol-Version": "1", Authorization: `Bearer ${a.token}` }, data: input });
    if (!requested.ok()) throw new Error(`RequestCallMigration ${requested.status()}`);
    for (const person of people) await until(() => person.page.evaluate(() => window.axonCallManager.snapshot().call?.migration?.phase === "completed"));
    const committed = await a.page.evaluate(() => { const c = window.axonCallManager.snapshot().call; return { id: c.id, started: String(c.startedAt.seconds) }; });
    if (JSON.stringify(committed) !== JSON.stringify(evidence.original)) throw new Error("Migration changed business call");
  }
  for (const person of people) {
    await until(() => person.page.evaluate(others => { const v = window.axonCallManager.snapshot(); return v.call?.state === 3 && v.media?.room?.remoteParticipants.size === others; }, people.length - 1));
    await person.page.evaluate(() => { window.axonSFURetained = window.axonCallManager.snapshot().media; });
  }
  evidence.callID = await a.page.evaluate(() => window.axonCallManager.snapshot().call.id);
  evidence.room = `call_${evidence.callID}`;
  evidence.initial = await Promise.all(people.map(snapshot));
  await a.page.evaluate(() => {
    const client = window.axonSFURetained.room.engine.client, original = client.onTokenRefresh;
    window.axonSFURefresh = [];
    client.onTokenRefresh = token => {
      original?.call(client, token);
      window.axonSFURefreshedToken = token;
      const claims = JSON.parse(atob(token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/")));
      window.axonSFURefresh.push({ at: Date.now(), identity: claims.sub, room: claims.video.room, exp: claims.exp });
    };
  });
  let checkpoint = Date.now();
  const refreshDeadline = Math.min(started + limit - 30000, Date.now() + 11 * 60 * 1000);
  while (Date.now() < refreshDeadline) {
    evidence.refresh = await a.page.evaluate(() => window.axonSFURefresh);
    if (evidence.refresh.length) break;
    if (Date.now() - checkpoint >= 60000) { console.log(`Waiting for actual server refresh: ${Math.floor((Date.now() - started) / 1000)}s`); checkpoint = Date.now(); save(); }
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  if (!evidence.refresh?.length) throw new Error("Actual server refresh not observed");
  const refreshed = await a.page.evaluate(() => window.axonSFURefreshedToken);
  for (const claims of evidence.refresh) { if (claims.identity.split(":").length !== 4 || claims.room !== evidence.room || claims.identity !== a.sockets[0].identity) throw new Error("Refreshed binding mismatch"); }
  console.log("Actual bound token refresh observed"); save();
  for (const scenario of ["signal-reconnect", "full-reconnect"]) {
    const count = a.sockets.length, at = Date.now();
    await a.page.evaluate(value => window.axonSFURetained.room.simulateScenario(value), scenario);
    await until(() => a.sockets.length > count);
    await until(() => a.page.evaluate(others => { const v = window.axonCallManager.snapshot(); return v.call?.state === 3 && v.media?.room?.state === "connected" && v.media.room.remoteParticipants.size === others; }, people.length - 1));
    const baseline = await snapshot(a);
    await until(async () => { const next = await snapshot(a); return next.audioBytes > baseline.audioBytes && next.energy > baseline.energy && next.frames > baseline.frames; });
    const ws = a.sockets.at(-1);
    if (ws.identity !== evidence.refresh[0].identity || ws.exp < evidence.refresh[0].exp) throw new Error("Reconnect did not use refreshed bound token");
    evidence.reconnect.push({ scenario, elapsedMs: Date.now() - at, websocket: ws, baseline, after: await snapshot(a) }); save();
  }
  evidence.beforeLogout = (await participants(evidence.room)).map(p => ({ identity: p.identity, sid: p.sid }));
  if (evidence.beforeLogout.length !== people.length) throw new Error("Unexpected server participant count");
  const revokeAt = Date.now();
  const logout = await a.context.request.post(`${gateway}/axon.auth.v1.AuthService/Logout`, { headers: { Origin: origin, "Connect-Protocol-Version": "1" }, data: {} });
  if (!logout.ok()) throw new Error(`Logout ${logout.status()}`);
  await until(async () => !(await participants(evidence.room)).some(p => p.identity === a.sockets[0].identity));
  evidence.revokeRemovalMs = Date.now() - revokeAt;
  for (const path of ["/rtc", "/rtc/validate", "/rtc/v1", "/rtc/v1/validate"]) {
    const response = await b.context.request.get(`${gateway}/livekit${path}?access_token=${encodeURIComponent(refreshed)}`, { headers: { Origin: origin } });
    if (response.status() !== 403) throw new Error(`Revoked refreshed token accepted ${path}`);
    evidence.denied.push({ path, status: response.status() });
  }
  await until(() => a.page.evaluate(() => [...window.axonSFURetained.tracks].every(t => t.readyState === "ended")));
  if (!migration) {
    await until(() => b.page.evaluate(oldOwner => { const owner = window.axonCallManager.snapshot().call?.ownerId; return !!owner && owner !== oldOwner; }, a.userID));
    const remainingBaseline = await snapshot(b);
    await until(async () => (await snapshot(b)).audioBytes > remainingBaseline.audioBytes);
    evidence.remaining = [await snapshot(b), await snapshot(c)];
    const ownerID = await b.page.evaluate(() => window.axonCallManager.snapshot().call.ownerId);
    const owner = [b, c].find(person => person.userID === ownerID);
    if (!owner) throw new Error("No accepted successor owner");
    await owner.page.getByRole("button", { name: "End for everyone", exact: true }).click();
  }
  for (const person of people) await until(() => person.page.evaluate(() => window.axonSFURetained.room.state === "disconnected" && [...window.axonSFURetained.tracks].every(t => t.readyState === "ended")));
  evidence.cleanup = await Promise.all(people.map(snapshot));
  if (migration) for (const person of people) {
    const source = await person.page.evaluate(() => ({ state: window.axonMigrationSource.pc.connectionState, ended: [...window.axonMigrationSource.tracks].every(t => t.readyState === "ended") }));
    if (source.state !== "closed" || !source.ended) throw new Error("Original P2P resources survived migration");
  }
  evidence.passed = true; evidence.elapsedMs = Date.now() - started; save();
  console.log(`Production refresh/reconnect/logout PASS ${evidence.elapsedMs}ms; observed removal ${evidence.revokeRemovalMs}ms`);
} catch (error) { evidence.passed = false; evidence.error = error.message; save(); console.error(error.message); process.exitCode = 1; }
finally { clearTimeout(watchdog); for (const person of people) { await person.page.evaluate(() => window.axonCallManager?.end()).catch(() => undefined); await person.context.close(); } await browser.close(); }
