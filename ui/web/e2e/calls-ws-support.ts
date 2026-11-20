import http from "node:http";
import { fromJson } from "@bufbuild/protobuf";
import { RegisterResponseSchema } from "../src/gen/axon/auth/v1/auth_pb";
import { expect, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { newEmail, newUsername, password } from "./support";
import { startChat } from "./chat-support";

const gateway = "http://localhost:8080";
export async function callPeers(browser: Browser, mode: "probe" | "ui" = "probe", count = 2) {
  const contexts: BrowserContext[] = [];
  try {
    const people = [];
    for (const label of ["Call Alice", "Call Bob", "Call Carol", "Call Dave"].slice(0, count)) {
      const context = await browser.newContext({ permissions: ["microphone", "camera"] });
      contexts.push(context);
      const email = newEmail("calls");
      const username = newUsername("calls");
      const response = await context.request.post(gateway + "/axon.auth.v1.AuthService/Register", {
        headers: { "Connect-Protocol-Version": "1", Origin: "http://localhost:3000" }, data: { email, username, password, displayName: label },
      });
      expect(response.ok(), `Registration HTTP ${response.status()}`).toBe(true);
      const body = fromJson(RegisterResponseSchema, await response.json());
      expect(body.tokens?.accessToken.length).toBeGreaterThan(0);
      const page = await context.newPage();
      const network = { opened: 0, closed: 0, sockets: [] as { url: string; closed: boolean }[] };
      page.on("websocket", socket => {
        network.opened++;
        const state = { url: socket.url(), closed: false }; network.sockets.push(state);
        socket.on("close", () => { network.closed++; state.closed = true; });
      });
      people.push({ context, page, network, email, username, name: label, userID: body.user!.id, token: body.tokens!.accessToken });
    }
    const [a, b] = people;
    await a.page.goto("http://localhost:3000/chat");
    const roomID = await startChat(a.page, b, "Calls signaling fixture");
    if (people[2]) {
      await startChat(a.page, people[2], "Group call contact fixture");
      await a.page.goto(`http://localhost:3000/chat#${b.username}`);
    }
    if (mode === "probe") {
      await Promise.all(people.map(person => person.page.goto("http://localhost:3000/calls/probe")));
      for (const person of people) {
        await expect.poll(() => person.page.evaluate(() => window.axonCallsProbe?.userID)).toBe(person.userID);
        await person.page.evaluate(() => window.axonCallsProbe!.channel.register());
        await expect.poll(() => person.page.evaluate(() => window.axonCallsProbe!.snapshots)).toBeGreaterThan(0);
      }
    } else {
      for (const person of people.slice(1)) await person.page.goto("http://localhost:3000/calls");
      for (const person of people) await expect.poll(() => person.page.evaluate(() => window.axonCallManager?.snapshot().available)).toBe(true);
    }
    return { a, b, c: people[2], d: people[3], roomID, close: () => Promise.all(contexts.map(context => context.close())) };
  } catch (error) { await Promise.all(contexts.map(context => context.close())); throw error; }
}

export async function callState(page: Page, callID: string) {
  return page.evaluate(id => window.axonCallsProbe!.calls.get(id)?.state, callID);
}

function docker<T>(method: string, pathname: string, body?: object): Promise<T> {
  return new Promise((resolve, reject) => {
    const payload = body ? JSON.stringify(body) : undefined;
    const request = http.request({ socketPath: "/var/run/docker.sock", path: pathname, method, headers: payload ? { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(payload) } : undefined }, response => {
      const chunks: Buffer[] = [];
      response.on("data", chunk => chunks.push(chunk));
      response.on("end", () => {
        const data = Buffer.concat(chunks);
        if ((response.statusCode ?? 500) >= 300) return reject(new Error(`Docker API ${response.statusCode}`));
        if (response.headers["content-type"]?.includes("json")) resolve(JSON.parse(data.toString()) as T);
        else resolve(data as T);
      });
    });
    request.on("error", reject); request.end(payload);
  });
}

export async function callSQL(query: string) {
  if (!query.trimStart().startsWith("SELECT")) throw new Error("Evidence query must be read-only");
  const filters = encodeURIComponent(JSON.stringify({ label: ["com.docker.compose.project=axon-calls-e2e", "com.docker.compose.service=postgres"] }));
  const containers = await docker<{ Id: string }[]>("GET", `/containers/json?filters=${filters}`);
  if (containers.length !== 1) throw new Error("Isolated Calls Postgres not found");
  const exec = await docker<{ Id: string }>("POST", `/containers/${containers[0].Id}/exec`, { AttachStdout: true, AttachStderr: true, Cmd: ["psql", "-U", "axon", "-d", "axon", "-tAc", query] });
  const raw = await docker<Buffer>("POST", `/exec/${exec.Id}/start`, { Detach: false, Tty: false });
  let output = "";
  for (let at = 0; at + 8 <= raw.length;) { const length = raw.readUInt32BE(at + 4); output += raw.subarray(at + 8, at + 8 + length).toString(); at += length + 8; }
  const status = await docker<{ ExitCode: number }>("GET", `/exec/${exec.Id}/json`);
  if (status.ExitCode) throw new Error(output);
  return output.trim();
}

export async function stopCallsRedis() {
  const filters = encodeURIComponent(JSON.stringify({ label: ["com.docker.compose.project=axon-calls-e2e", "com.docker.compose.service=redis"] }));
  const containers = await docker<{ Id: string }[]>("GET", `/containers/json?filters=${filters}`);
  if (containers.length !== 1) throw new Error("Isolated Calls Redis not found");
  const id = containers[0].Id;
  await docker("POST", `/containers/${id}/stop?t=1`);
  const state = await docker<{ State: { Running: boolean } }>("GET", `/containers/${id}/json`);
  expect(state.State.Running).toBe(false);
  return async () => {
    await docker("POST", `/containers/${id}/start`);
    const state = await docker<{ State: { Running: boolean } }>("GET", `/containers/${id}/json`);
    expect(state.State.Running).toBe(true);
  };
}

export async function restartCallsChat() {
  const filters = encodeURIComponent(JSON.stringify({ label: ["com.docker.compose.project=axon-calls-e2e", "com.docker.compose.service=chat"] }));
  const containers = await docker<{ Id: string }[]>("GET", `/containers/json?filters=${filters}`);
  if (containers.length !== 1) throw new Error("Isolated Calls Chat not found");
  await docker("POST", `/containers/${containers[0].Id}/restart?t=0`);
}
