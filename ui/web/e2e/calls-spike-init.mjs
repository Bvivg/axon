import { execFileSync } from "node:child_process";
import { createHmac, generateKeyPairSync, randomBytes } from "node:crypto";
import fs from "node:fs";

const directory = "/evidence/.secrets";
fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
const openssl = (...args) => execFileSync("openssl", args, { stdio: "ignore" });

if (!fs.existsSync(`${directory}/ca.crt`)) {
  openssl("req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "2", "-subj", "/CN=AXON Calls Spike CA", "-keyout", `${directory}/ca.key`, "-out", `${directory}/ca.crt`);
  openssl("req", "-newkey", "rsa:2048", "-nodes", "-subj", "/CN=turn.calls.test", "-keyout", `${directory}/turn.key`, "-out", `${directory}/turn.csr`);
  fs.writeFileSync(`${directory}/turn.ext`, "subjectAltName=DNS:turn.calls.test\nextendedKeyUsage=serverAuth\n");
  openssl("x509", "-req", "-days", "2", "-in", `${directory}/turn.csr`, "-CA", `${directory}/ca.crt`, "-CAkey", `${directory}/ca.key`, "-CAcreateserial", "-extfile", `${directory}/turn.ext`, "-out", `${directory}/turn.crt`);
}

const existingConfig = fs.existsSync(`${directory}/turnserver.conf`) ? fs.readFileSync(`${directory}/turnserver.conf`, "utf8") : "";
const secret = existingConfig.match(/^static-auth-secret=(.+)$/m)?.[1] ?? randomBytes(32).toString("hex");
const username = `${Math.floor(Date.now() / 1000) + 3600}:axon-spike`;
const listenIP = process.env.TURN_LISTEN_IP ?? "172.30.251.20";
const allowedPeers = process.env.TURN_ALLOWED_PEERS ?? "172.30.251.0-172.30.251.255";
const credential = createHmac("sha1", secret).update(username).digest("base64");
fs.writeFileSync(`${directory}/credentials.json`, JSON.stringify({ username, credential }), { mode: 0o600 });
fs.writeFileSync(`${directory}/turnserver.conf`, [
  "listening-port=3478", "tls-listening-port=5349", `listening-ip=${listenIP}`, `relay-ip=${listenIP}`,
  "min-port=49160", "max-port=49220", "realm=turn.calls.test", "fingerprint", "use-auth-secret", `static-auth-secret=${secret}`,
  "cert=/evidence/.secrets/turn.crt", "pkey=/evidence/.secrets/turn.key", "no-cli", "no-multicast-peers", "no-dtls",
  "no-tlsv1", "no-tlsv1_1", "no-tcp-relay", "stale-nonce=600", "user-quota=12", "total-quota=32",
  "denied-peer-ip=0.0.0.0-255.255.255.255", "denied-peer-ip=::-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
  `allowed-peer-ip=${allowedPeers}`, "log-file=stdout", "simple-log", "verbose", "pidfile=/tmp/turnserver.pid",
].join("\n") + "\n", { mode: 0o600 });

const rate = 48000;
const audio = Buffer.alloc(44 + rate * 2);
audio.write("RIFF", 0); audio.writeUInt32LE(audio.length - 8, 4); audio.write("WAVEfmt ", 8);
audio.writeUInt32LE(16, 16); audio.writeUInt16LE(1, 20); audio.writeUInt16LE(1, 22);
audio.writeUInt32LE(rate, 24); audio.writeUInt32LE(rate * 2, 28); audio.writeUInt16LE(2, 32); audio.writeUInt16LE(16, 34);
audio.write("data", 36); audio.writeUInt32LE(rate * 2, 40);
for (let i = 0; i < rate; i++) audio.writeInt16LE(Math.round(Math.sin(2 * Math.PI * 440 * i / rate) * 12000), 44 + i * 2);
fs.writeFileSync(`${directory}/tone.wav`, audio);
if (!fs.existsSync(`${directory}/auth-key.pem`)) {
  const pair = generateKeyPairSync("rsa", { modulusLength: 2048, privateKeyEncoding: { type: "pkcs8", format: "pem" }, publicKeyEncoding: { type: "spki", format: "pem" } });
  fs.writeFileSync(`${directory}/auth-key.pem`, pair.privateKey, { mode: 0o600 });
}
fs.writeFileSync(`${directory}/calls.env`, `CALLS_TURN_SECRET=${secret}\nJWT_PRIVATE_KEY_DEV_1=${fs.readFileSync(`${directory}/auth-key.pem`).toString("base64")}\n`, { mode: 0o600 });
console.log("Generated disposable TURN credentials, trusted test CA and 440 Hz capture fixture.");
