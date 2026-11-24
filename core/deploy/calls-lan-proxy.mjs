import fs from "node:fs";
import http from "node:http";
import https from "node:https";
import net from "node:net";

const directory = "/secrets";
const lanHosts = (process.env.LAN_HOSTS || "192.168.2.1,10.80.10.122,localhost").split(",");
const hosts = new Set(lanHosts.map(host => `${host}:3443`));
for (const host of lanHosts) { hosts.add(host); hosts.add(`${host}:443`); }
hosts.add("localhost:3003"); hosts.add("127.0.0.1:3003");
hosts.add("axon.test"); hosts.add("axon.test:443"); hosts.add("axon.test:3443");
const mediaPath = "/api/media/";
const avatarObject = /^\/storage(\/avatars\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\/(?:small|medium|large|original)\.jpg)$/;
const storageHeaders = ["accept", "accept-encoding", "if-none-match", "if-modified-since", "range", "user-agent"];
const gatewayHost = process.env.GATEWAY_HOST || "gateway";
const nativeRPCPath = /^\/axon\.[a-z][a-z0-9]*\.v[0-9]+\.[A-Za-z][A-Za-z0-9]*\/[A-Za-z][A-Za-z0-9]*$/;
const nativeSocketPaths = new Set(["/ws/chat", "/ws/game", "/ws/presence", "/livekit/rtc", "/livekit/rtc/v1"]);
const nativeMediaValidationPath = /^\/livekit\/rtc(?:\/v1)?\/validate$/;
const nativeUploadPath = /^\/api\/(?:avatar$|attachment$|uploads(?:\/|$))/;
const target = url => {
  const [path] = url.split("?", 1);
  if (nativeRPCPath.test(path) || nativeSocketPaths.has(path) || nativeMediaValidationPath.test(path) || nativeUploadPath.test(path) || path.startsWith(mediaPath)) {
    return { host: gatewayHost, port: 8080, path: url };
  }
  if (path.startsWith("/api/")) {
    return { host: gatewayHost, port: 8080, path: url.slice(4) };
  }
  if (path.startsWith("/storage/")) {
    return { host: "minio", port: 9000, path: path.slice(8), storage: true };
  }
  return { host: "web", port: 3000, path: url };
};
const hotspotPAC = `function FindProxyForURL(url, host) {
  if (host.toLowerCase() === "axon.test" && /^(https|wss):/.test(url)) {
    return ${JSON.stringify(`PROXY ${lanHosts[0]}:3444`)};
  }
  return "DIRECT";
}
`;
const handler = (request, response) => {
  if (!hosts.has(request.headers.host)) { response.writeHead(421); response.end(); return; }
  if (request.method === "GET" && request.socket.encrypted && request.url === "/axon-test.pac") {
    response.writeHead(200, { "Content-Type": "application/x-ns-proxy-autoconfig", "Cache-Control": "no-store" });
    response.end(hotspotPAC);
    return;
  }
  if (request.url?.startsWith("/api" + mediaPath)) {
    response.writeHead(308, { Location: request.url.slice(4), "Cache-Control": "private, no-store" });
    response.end();
    return;
  }
  if (request.url?.startsWith("/storage/") && !((request.method === "GET" || request.method === "HEAD") && avatarObject.test(request.url))) {
    response.writeHead(404, { "Cache-Control": "no-store" });
    response.end();
    return;
  }
  const upstream = target(request.url || "/");
  const headers = upstream.storage
    ? Object.fromEntries(storageHeaders.filter(name => request.headers[name]).map(name => [name, request.headers[name]]))
    : { ...request.headers, "x-forwarded-proto": request.socket.encrypted ? "https" : "http", "x-forwarded-for": request.socket.remoteAddress, "x-forwarded-host": request.headers.host };
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
  if (!hosts.has(request.headers.host) || !(nativeSocketPaths.has(path) || media || path?.startsWith("/api/ws/") || path?.startsWith("/_next/webpack-hmr"))) { socket.destroy(); return; }
  const upstream = target(request.url);
  const proxy = http.request({ hostname: upstream.host, port: upstream.port, path: upstream.path, method: "GET", headers: { ...request.headers, "x-forwarded-for": request.socket.remoteAddress } });
  proxy.on("upgrade", (response, remote, pending) => {
    socket.write(`HTTP/1.1 101 Switching Protocols\r\n${Object.entries(response.headers).map(([name, value]) => `${name}: ${value}`).join("\r\n")}\r\n\r\n`);
    if (pending.length) socket.write(pending);
    if (head.length) remote.write(head);
    remote.pipe(socket); socket.pipe(remote);
    socket.on("error", () => remote.destroy()); remote.on("error", () => socket.destroy());
    socket.on("close", () => remote.destroy()); remote.on("close", () => socket.destroy());
  });
  proxy.on("response", response => {
    response.resume();
    if (nativeSocketPaths.has(path)) {
      socket.end(`HTTP/1.1 ${response.statusCode} ${response.statusMessage}\r\nConnection: close\r\nContent-Length: 0\r\n\r\n`);
    } else {
      socket.destroy();
    }
  });
  proxy.on("error", () => socket.destroy()); proxy.end();
};
server.on("upgrade", upgrade); localServer.on("upgrade", upgrade);
server.listen(3443, "0.0.0.0");
localServer.listen(3003, "0.0.0.0");
const setupServer = http.createServer((request, response) => {
  if (request.url === "/axon-dns.mobileconfig") {
    response.writeHead(200, { "Content-Type": "application/x-apple-aspen-config", "Content-Disposition": "attachment; filename=axon-dns.mobileconfig", "Cache-Control": "no-store" });
    response.end(fs.readFileSync("/axon-dns.mobileconfig")); return;
  }
  if (request.url !== "/axon-ca.crt") { response.writeHead(404); response.end(); return; }
  response.writeHead(200, { "Content-Type": "application/x-x509-ca-cert", "Content-Disposition": "attachment; filename=axon-ca.crt", "Cache-Control": "no-store" });
  response.end(fs.readFileSync(`${directory}/ca.crt`));
});
setupServer.on("connect", (request, client, head) => {
  if (request.url !== "axon.test:443") {
    client.end("HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n");
    return;
  }

  const upstream = net.connect(3443, "127.0.0.1");
  client.on("error", () => upstream.destroy());
  client.on("close", () => upstream.destroy());
  upstream.on("error", () => client.destroy());
  upstream.on("close", () => client.destroy());
  upstream.once("connect", () => {
    client.write("HTTP/1.1 200 Connection Established\r\n\r\n");
    if (head.length) upstream.write(head);
    client.pipe(upstream);
    upstream.pipe(client);
  });
});
setupServer.listen(3444, "0.0.0.0");
