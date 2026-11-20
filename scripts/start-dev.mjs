import { spawn, spawnSync } from "node:child_process";
import { generateKeyPairSync, randomBytes } from "node:crypto";
import { createWriteStream, existsSync, mkdirSync, readFileSync, readdirSync, unlinkSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { homedir } from "node:os";
import { delimiter, dirname, join, resolve } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import { parseEnv } from "node:util";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const supported = (version) => Number(version.split(".")[0]) >= 24;
if (!supported(process.versions.node)) {
  const versions = join(process.env.NVM_DIR || join(homedir(), ".nvm"), "versions/node");
  const candidate = existsSync(versions) && readdirSync(versions).filter((name) => supported(name.slice(1))).sort((a, b) => b.localeCompare(a, undefined, { numeric: true })).map((name) => join(versions, name, "bin/node")).find(existsSync);
  if (!candidate) {
    console.error("start:dev requires Node.js 24+: install it, then run nvm use.");
    process.exit(1);
  }
  const child = spawn(candidate, [fileURLToPath(import.meta.url)], { stdio: "inherit", env: { ...process.env, PATH: `${dirname(candidate)}${delimiter}${process.env.PATH}` } });
  for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => child.kill(signal));
  child.on("exit", (code, signal) => process.exit(code ?? (signal ? 1 : 0)));
  child.on("error", () => process.exit(1));
} else {
  await main();
}

async function main() {
  const runtime = join(root, ".local/dev");
  mkdirSync(runtime, { recursive: true });
  const children = new Set();
  const abort = new AbortController();
  let stopping = false;
  let failed = false;
  let secrets = [];
  const redact = (value) => secrets.reduce((text, secret) => text.replaceAll(secret, "[redacted]"), value.replace(/postgres(?:ql)?:\/\/[^\s]+/g, "[database URL]"));
  const log = (name, value) => console.log(`[${name}] ${redact(value)}`);
  const sleep = (ms) => new Promise((done) => setTimeout(done, ms));
  const signalGroup = (child, signal) => {
    try { process.kill(-child.pid, signal); } catch (error) { if (error.code !== "ESRCH") throw error; }
  };
  const stop = async (code = 0) => {
    if (stopping) return;
    stopping = true;
    abort.abort();
    process.exitCode = code;
    log("dev", "Stopping local application; infrastructure stays running.");
    const active = [...children];
    for (const child of active) signalGroup(child, "SIGTERM");
    const alive = (child) => {
      try { process.kill(-child.pid, 0); return true; } catch (error) { if (error.code === "ESRCH") return false; throw error; }
    };
    const deadline = Date.now() + 20_000;
    while (active.some(alive) && Date.now() < deadline) await sleep(100);
    for (const child of active.filter(alive)) signalGroup(child, "SIGKILL");
    const pids = join(runtime, "start-dev-pids.json");
    if (existsSync(pids) && JSON.parse(readFileSync(pids, "utf8")).supervisor === process.pid) unlinkSync(pids);
  };
  process.on("SIGINT", () => void stop());
  process.on("SIGTERM", () => void stop());

  function launch(name, command, args, options = {}) {
    if (stopping) throw new Error("Startup interrupted");
    const output = createWriteStream(join(runtime, `${name}.log`), { flags: "a" });
    const child = spawn(command, args, { cwd: root, env, ...options, detached: true, stdio: ["ignore", "pipe", "pipe"] });
    children.add(child);
    for (const stream of [child.stdout, child.stderr]) {
      createInterface({ input: stream }).on("line", (line) => { const clean = redact(line); log(name, clean); output.write(`${clean}\n`); });
    }
    const completed = new Promise((done, reject) => {
      child.once("error", () => { children.delete(child); output.end(); reject(new Error(`${name}: could not start ${command}`)); });
      child.once("exit", (code, signal) => {
        if (options.persistent && !stopping) {
          failed = true;
          log(name, `Unexpected exit (${code ?? signal})`);
          void stop(1);
        }
      });
      child.once("close", (code, signal) => {
        children.delete(child);
        output.end();
        if (code === 0 || stopping) done();
        else reject(new Error(`${name} failed (${code ?? signal}); see .local/dev/${name}.log`));
      });
    });
    return { child, completed };
  }

  const envFile = join(root, "core/deploy/.env");
  const defaults = parseEnv(readFileSync(join(root, "core/deploy/.env.example"), "utf8"));
  if (!existsSync(envFile)) writeFileSync(envFile, readFileSync(join(root, "core/deploy/.env.example")), { mode: 0o600 });
  const overrides = join(root, ".env.local");
  const env = { ...process.env, ...defaults, ...parseEnv(readFileSync(envFile, "utf8")), ...(existsSync(overrides) ? parseEnv(readFileSync(overrides, "utf8")) : {}), ...process.env };
  env.PATH = `${dirname(process.execPath)}${delimiter}${process.env.PATH}`;
  env.APP_ENV = "development";
  env.NEXT_TELEMETRY_DISABLED = "1";

  try {
    for (const tool of ["go", "ffmpeg", "ffprobe", ...(env.AXON_DEV_INFRA === "external" ? [] : ["docker"])]) {
      if (spawnSync(tool, [tool === "docker" ? "--version" : tool === "go" ? "version" : "-version"], { stdio: "ignore" }).status !== 0) throw new Error(`Missing dependency: ${tool}`);
    }
    const ports = { web: Number(env.DEV_WEB_PORT || 3000), gateway: Number(env.DEV_GATEWAY_PORT || 28080), auth: Number(env.DEV_AUTH_PORT || 28081), chat: Number(env.DEV_CHAT_PORT || 28082), game: Number(env.DEV_GAME_PORT || 28083), rating: Number(env.DEV_RATING_PORT || 28084), authAdmin: Number(env.DEV_AUTH_ADMIN_PORT || 29091), chatAdmin: Number(env.DEV_CHAT_ADMIN_PORT || 29092), gatewayAdmin: Number(env.DEV_GATEWAY_ADMIN_PORT || 29090), ratingAdmin: Number(env.DEV_RATING_ADMIN_PORT || 29094) };
    if (new Set(Object.values(ports)).size !== Object.values(ports).length) throw new Error("DEV_* ports must be distinct");
    for (const [name, port] of Object.entries(ports)) {
      if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error(`Invalid port for ${name}`);
      await new Promise((done, reject) => {
        const server = createServer();
        server.once("error", () => reject(new Error(`Port ${port} (${name}) is occupied; stop the previous dev process or set DEV_*_PORT in .env.local`)));
        server.listen(port, "127.0.0.1", () => server.close(done));
      });
    }
    const keyID = env.JWT_ACTIVE_KEY_ID || "dev-1";
    const keyName = `JWT_PRIVATE_KEY_${keyID.toUpperCase().replaceAll(/[-.]/g, "_")}`;
    env.JWT_ACTIVE_KEY_ID = keyID;
    if (!env[keyName]) {
      env[keyName] = Buffer.from(generateKeyPairSync("rsa", { modulusLength: 2048, privateKeyEncoding: { type: "pkcs8", format: "pem" }, publicKeyEncoding: { type: "spki", format: "pem" } }).privateKey).toString("base64");
      writeFileSync(envFile, `\n${keyName}=${env[keyName]}\n`, { flag: "a", mode: 0o600 });
    }
    if (!env.JWT_KEY_ENCRYPTION_KEY) {
      env.JWT_KEY_ENCRYPTION_KEY = randomBytes(32).toString("base64");
      writeFileSync(envFile, `\nJWT_KEY_ENCRYPTION_KEY=${env.JWT_KEY_ENCRYPTION_KEY}\n`, { flag: "a", mode: 0o600 });
    }
    secrets = Object.entries(env).filter(([key, value]) => /PASSWORD|SECRET|PRIVATE_KEY|INTERNAL_KEY|ENCRYPTION_KEY/.test(key) && value.length >= 8).map(([, value]) => value);

    if (env.AXON_DEV_INFRA !== "external") {
      const listing = spawnSync("docker", ["compose", "ls", "--all", "--format", "json"], { encoding: "utf8" });
      if (listing.status !== 0) throw new Error("Docker is unavailable; start Docker Desktop");
      const project = JSON.parse(listing.stdout).find((item) => item.Name === "axon");
      const files = project ? project.ConfigFiles.split(",") : [join(root, "core/deploy/docker-compose.yml")];
      if (!files.includes(join(root, "core/deploy/docker-compose.yml"))) throw new Error("Another checkout owns the axon Compose project; stop it before starting this checkout");
      const compose = ["compose", "--project-name", "axon", "--project-directory", join(root, "core/deploy"), ...files.flatMap((file) => ["-f", file])];
      await launch("infrastructure", "docker", [...compose, "up", "-d", "--wait", "--wait-timeout", "180", "--no-recreate", "postgres", "redis", "kafka", "minio"]).completed;
      await launch("storage-init", "docker", [...compose, "run", "--rm", "--no-deps", "minio-init"]).completed;
      await launch("schemas", "docker", [...compose, "run", "--rm", "--no-deps", "postgres-schemas"]).completed;
      if (env.CALLS_SFU_ENABLED === "true" && !env.DEV_LIVEKIT_URL) {
        await launch("livekit", "docker", [...compose, "up", "-d", "--no-recreate", "livekit", "coturn", "livekit-bridge"]).completed;
      }
    }

    const web = join(root, "ui/web");
    const npm = join(dirname(process.execPath), "npm");
    const fingerprint = `${process.platform}/${process.arch}/${process.versions.node.split(".")[0]}\n${readFileSync(join(web, "package-lock.json"), "utf8")}`;
    const stamp = join(runtime, "web-dependencies");
    if (!existsSync(join(web, "node_modules/next/package.json")) || !existsSync(stamp) || readFileSync(stamp, "utf8") !== fingerprint || spawnSync(process.execPath, ["--input-type=module", "-e", "await import('vite')"], { cwd: web, stdio: "ignore" }).status !== 0) {
      await launch("install", npm, ["ci", "--include=optional", "--no-audit", "--no-fund"], { cwd: web }).completed;
      writeFileSync(stamp, fingerprint);
    }
    if (!existsSync(join(root, "core/shared/gen/go/axon/auth/v1/auth.pb.go")) || !existsSync(join(web, "src/gen/axon/auth/v1/auth_pb.ts"))) {
      await launch("proto", npm, ["exec", "--yes", "--package=@bufbuild/buf@1.75.0", "--", "buf", "generate"]).completed;
    }
    const pgHost = env.DEV_POSTGRES_HOST || "127.0.0.1";
    const localDSN = (value) => {
      const url = new URL(value);
      if (url.hostname === "postgres") { url.hostname = pgHost; url.port = env.POSTGRES_HOST_PORT; }
      return url.href;
    };
    for (const service of ["auth", "chat", "game", "rating"]) {
      const dsn = env[`${service.toUpperCase()}_POSTGRES_DSN`];
      await launch(`migrate-${service}`, "go", ["run", "-tags", "postgres", "github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1", "-path", join(root, `core/services/${service}/migrations`), "-database", localDSN(dsn), "up"], { cwd: runtime, env: { ...env, GOWORK: "off" } }).completed;
    }
    const origin = `http://localhost:${ports.web}`;
    const api = `http://localhost:${ports.gateway}`;
    const auth = `http://127.0.0.1:${ports.auth}`;
    const common = {
      ...env,
      REDIS_ADDR: env.REDIS_ADDR.startsWith("redis:") ? `127.0.0.1:${env.REDIS_HOST_PORT}` : env.REDIS_ADDR,
      AUTH_SERVICE_URL: auth,
      CHAT_SERVICE_URL: `http://127.0.0.1:${ports.chat}`,
      GAME_SERVICE_URL: `http://127.0.0.1:${ports.game}`,
      JWKS_URL: `http://127.0.0.1:${ports.authAdmin}/.well-known/jwks.json`,
      CORS_ALLOWED_ORIGINS: `${origin},http://127.0.0.1:${ports.web}`,
      OAUTH_ALLOWED_RETURN_ORIGINS: `${origin},http://127.0.0.1:${ports.web}`,
      OAUTH_REDIRECT_BASE_URL: `${origin}/auth/callback`,
      OAUTH_FAKE_AUTHORIZE_URL: `${auth}/oauth/fake/authorize`,
      NEXT_PUBLIC_API_BASE_URL: api,
      AVATARS_ENDPOINT: env.DEV_S3_ENDPOINT || `127.0.0.1:${env.MINIO_HOST_PORT}`,
      AVATARS_PUBLIC_URL: env.DEV_S3_PUBLIC_URL || `http://localhost:${env.MINIO_HOST_PORT}`,
      ATTACHMENTS_ENDPOINT: env.DEV_S3_ENDPOINT || `127.0.0.1:${env.MINIO_HOST_PORT}`,
      ATTACHMENTS_PUBLIC_URL: env.DEV_S3_PUBLIC_URL || `http://localhost:${env.MINIO_HOST_PORT}`,
      KAFKA_BROKERS: env.DEV_KAFKA_BROKERS || `127.0.0.1:${env.KAFKA_HOST_PORT}`,
      LIVEKIT_API_URL: env.DEV_LIVEKIT_URL || "http://127.0.0.1:17880",
      LIVEKIT_SIGNAL_URL: env.DEV_LIVEKIT_URL || "http://127.0.0.1:17880",
      LIVEKIT_PUBLIC_URL: `${api}/livekit`,
      REFRESH_COOKIE_SECURE: "false",
      TRUSTED_PROXIES: "0",
    };
    async function ready(url) {
      const deadline = Date.now() + 120_000;
      while (!stopping && Date.now() < deadline) {
        try { if ((await fetch(url, { signal: AbortSignal.any([abort.signal, AbortSignal.timeout(3000)]) })).ok) return; } catch {}
        await sleep(300);
      }
      throw new Error(stopping ? "Startup interrupted" : `Readiness timeout: ${url}`);
    }
    for (const service of ["auth", "chat", "game", "rating", "gateway"]) {
      const serviceEnv = { ...common, HTTP_ADDR: `127.0.0.1:${ports[service]}`, METRICS_ADDR: `127.0.0.1:${ports[`${service}Admin`] || 0}` };
      if (service !== "gateway") serviceEnv.POSTGRES_DSN = localDSN(env[`${service.toUpperCase()}_POSTGRES_DSN`]);
      if (service === "game") serviceEnv.GAME_ADVERTISE_ADDR = `127.0.0.1:${ports.game}`;
      const task = launch(service, "go", ["run", `./services/${service}/cmd/${service}`], { cwd: join(root, "core"), env: serviceEnv, persistent: true });
      task.completed.catch(() => {});
      await ready(`http://127.0.0.1:${service === "game" ? ports.game : ports[`${service}Admin`]}/readyz`);
    }
    const frontend = launch("web", npm, ["run", "dev", "--", "--port", String(ports.web)], { cwd: web, env: { ...common, AXON_GATEWAY_RPC_URL: api, AXON_GATEWAY_METRICS_URL: `http://127.0.0.1:${ports.gatewayAdmin}`, AXON_GATEWAY_ENVIRONMENT: "Local dev", ALLOWED_DEV_ORIGINS: "localhost,127.0.0.1" }, persistent: true });
    frontend.completed.catch(() => {});
    await ready(`${origin}/login`);
    writeFileSync(join(runtime, "start-dev-pids.json"), JSON.stringify({ supervisor: process.pid, children: [...children].map((child) => child.pid) }));
    log("dev", `Ready: ${origin} | Gateway: ${api} | Ctrl+C stops all local services.`);
  } catch (error) {
    if (!stopping) { failed = true; log("dev", error.message); }
    await stop(failed ? 1 : 0);
  }
}
