import fs from "node:fs";
import http from "node:http";
import https from "node:https";

const directory = "/secrets";
const hosts = new Set((process.env.LAN_HOSTS || "192.168.2.1,10.80.10.122,localhost").split(",").map(host => `${host}:3443`));
for (const host of (process.env.LAN_HOSTS || "192.168.2.1,10.80.10.122,localhost").split(",")) { hosts.add(host); hosts.add(`${host}:443`); }
hosts.add("localhost:3003"); hosts.add("127.0.0.1:3003");
hosts.add("axon.test"); hosts.add("axon.test:443"); hosts.add("axon.test:3443");
const mediaPath = "/api/media/";
const target = url => url.split("?")[0] === "/oauth/fake/authorize" ? { host: "auth", port: 8081, path: url } : url.startsWith("/api/") ? { host: "gateway", port: 8080, path: url.startsWith(mediaPath) ? url : url.slice(4) } : url.startsWith("/storage/") ? { host: "minio", port: 9000, path: url.slice(8) } : { host: "web", port: 3000, path: url };
const handler = (request, response) => {
  if (!hosts.has(request.headers.host)) { response.writeHead(421); response.end(); return; }
  if (request.url?.startsWith("/api" + mediaPath)) {
    response.writeHead(308, { Location: request.url.slice(4), "Cache-Control": "private, no-store" });
    response.end();
    return;
  }
  const upstream = target(request.url || "/");
  const headers = { ...request.headers, "x-forwarded-proto": request.socket.encrypted ? "https" : "http", "x-forwarded-for": request.socket.remoteAddress, "x-forwarded-host": request.headers.host };
  const proxy = http.request({ hostname: upstream.host, port: upstream.port, path: upstream.path, method: request.method, headers }, incoming => {
    if (request.url?.startsWith("/api/") && incoming.headers["set-cookie"]) incoming.headers["set-cookie"] = incoming.headers["set-cookie"].map(cookie => cookie.replace(/Path=\/axon\.auth\./gi, "Path=/api/axon.auth."));
    response.writeHead(incoming.statusCode || 502, incoming.headers); incoming.pipe(response);
  });
  proxy.setTimeout(30_000, () => proxy.destroy());
  proxy.on("error", () => { if (!response.headersSent) response.writeHead(502); response.end("Upstream unavailable"); });
  request.on("aborted", () => proxy.destroy()); request.pipe(proxy);
};
const server = https.createServer({ key: fs.readFileSync(`${directory}/server.key`), cert: fs.readFileSync(`${directory}/server.crt`), minVersion: "TLSv1.2" }, handler);
server.on("connection", socket => console.info("HTTPS connection", socket.remoteAddress));
server.on("secureConnection", socket => console.info("HTTPS TLS established", socket.remoteAddress, socket.getProtocol()));
server.on("tlsClientError", (error, socket) => console.warn("HTTPS TLS failed", socket.remoteAddress, error.code));
const localServer = http.createServer(handler);
const upgrade = (request, socket, head) => {
  const path = request.url?.split("?")[0];
  const media = path === "/api/livekit/rtc" || path === "/api/livekit/rtc/v1";
  if (!hosts.has(request.headers.host) || !(media || request.url?.startsWith("/api/ws/") || request.url?.startsWith("/_next/webpack-hmr"))) { socket.destroy(); return; }
  const upstream = target(request.url);
  const proxy = http.request({ hostname: upstream.host, port: upstream.port, path: upstream.path, method: "GET", headers: request.headers });
  proxy.on("upgrade", (response, remote, pending) => {
    socket.write(`HTTP/1.1 101 Switching Protocols\r\n${Object.entries(response.headers).map(([name, value]) => `${name}: ${value}`).join("\r\n")}\r\n\r\n`);
    if (pending.length) socket.write(pending);
    if (head.length) remote.write(head);
    remote.pipe(socket); socket.pipe(remote);
    socket.on("error", () => remote.destroy()); remote.on("error", () => socket.destroy());
    socket.on("close", () => remote.destroy()); remote.on("close", () => socket.destroy());
  });
  proxy.on("response", () => socket.destroy()); proxy.on("error", () => socket.destroy()); proxy.end();
};
server.on("upgrade", upgrade); localServer.on("upgrade", upgrade);
server.listen(3443, "0.0.0.0");
localServer.listen(3003, "0.0.0.0");
http.createServer((request, response) => {
  if (request.url === "/axon-dns.mobileconfig") {
    response.writeHead(200, { "Content-Type": "application/x-apple-aspen-config", "Content-Disposition": "attachment; filename=axon-dns.mobileconfig", "Cache-Control": "no-store" });
    response.end(fs.readFileSync("/axon-dns.mobileconfig")); return;
  }
  if (request.url !== "/axon-ca.crt") { response.writeHead(404); response.end(); return; }
  response.writeHead(200, { "Content-Type": "application/x-x509-ca-cert", "Content-Disposition": "attachment; filename=axon-ca.crt", "Cache-Control": "no-store" });
  response.end(fs.readFileSync(`${directory}/ca.crt`));
}).listen(3444,"0.0.0.0");
