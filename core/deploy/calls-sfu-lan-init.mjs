import fs from "node:fs";
import { randomBytes } from "node:crypto";

const directory = "/secrets";
const ip = process.env.AXON_LAN_IP || "192.168.2.1";
if (!/^192\.168\.\d{1,3}\.\d{1,3}$|^10\.\d{1,3}\.\d{1,3}\.\d{1,3}$/.test(ip)) throw new Error("Expected private LAN IPv4");
fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
const files = ["internal.env", "media.env", "livekit.yaml"];
const present = files.filter(file => fs.existsSync(`${directory}/${file}`));
if (present.length && present.length !== files.length) throw new Error("Incomplete SFU secrets; existing files were preserved");
if (present.length) {
  console.log("Existing LAN SFU credentials preserved");
} else {
  const internal = randomBytes(32).toString("hex"), key = "axon-lan", secret = randomBytes(32).toString("hex");
  fs.writeFileSync(`${directory}/internal.env`, `CALLS_INTERNAL_KEY=${internal}\n`, { mode: 0o600 });
  fs.writeFileSync(`${directory}/media.env`, `LIVEKIT_API_KEY=${key}\nLIVEKIT_API_SECRET=${secret}\n`, { mode: 0o600 });
  fs.writeFileSync(`${directory}/livekit.yaml`, `port: 7880\nbind_addresses: ["0.0.0.0"]\nrtc:\n  tcp_port: 7881\n  udp_port: 7882\n  node_ip: ${ip}\n  use_external_ip: false\nkeys:\n  ${key}: ${secret}\nroom:\n  auto_create: false\n  empty_timeout: 30\n  departure_timeout: 2\nwebhook:\n  api_key: ${key}\n  urls: ["http://chat:8082/internal/calls/livekit"]\nlogging:\n  level: info\n`, { mode: 0o600 });
  console.log(`LAN SFU fixtures generated for ${ip}; credentials redacted`);
}
