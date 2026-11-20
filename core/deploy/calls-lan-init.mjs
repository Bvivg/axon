import fs from "node:fs";
import { execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";

const directory = "/evidence/.secrets";
const primary = process.env.LAN_IP || "192.168.2.1";
const alternate = process.env.LAN_ALT_IP || "10.80.10.122";
for (const address of [primary, alternate]) if (!/^\d{1,3}(\.\d{1,3}){3}$/.test(address) || address.split(".").some(v => Number(v)>255)) throw new Error("Invalid LAN IP");
fs.mkdirSync(directory,{ recursive:true,mode:0o700 });
const openssl = (...args) => execFileSync("openssl", args, { stdio:"ignore" });
if (!fs.existsSync(`${directory}/ca.crt`)) openssl("req","-x509","-newkey","rsa:2048","-nodes","-days","365","-subj","/CN=AXON LAN CA","-keyout",`${directory}/ca.key`,"-out",`${directory}/ca.crt`);
openssl("req","-newkey","rsa:2048","-nodes","-subj",`/CN=${primary}`,"-keyout",`${directory}/server.key`,"-out",`${directory}/server.csr`);
fs.writeFileSync(`${directory}/server.ext`,`subjectAltName=IP:${primary},IP:${alternate},IP:127.0.0.1,DNS:localhost,DNS:axon.test\nextendedKeyUsage=serverAuth\n`);
openssl("x509","-req","-days","30","-in",`${directory}/server.csr`,"-CA",`${directory}/ca.crt`,"-CAkey",`${directory}/ca.key`,"-CAcreateserial","-extfile",`${directory}/server.ext`,"-out",`${directory}/server.crt`);
const existing = fs.existsSync(`${directory}/lan.env`) ? fs.readFileSync(`${directory}/lan.env`,"utf8") : "";
const secret = existing.match(/^CALLS_TURN_SECRET=(.+)$/m)?.[1] || randomBytes(32).toString("hex");
fs.writeFileSync(`${directory}/lan.env`,`CALLS_TURN_SECRET=${secret}\nCALLS_TURN_URLS=turn:${primary}:3478?transport=udp,turns:${primary}:5349?transport=tcp\n`,{mode:0o600});
const subnet = primary.split(".").slice(0,3).join(".");
fs.writeFileSync(`${directory}/turnserver.conf`,[
  "listening-port=3478","tls-listening-port=5349",`external-ip=${primary}`,"min-port=49160","max-port=49220","realm=axon.lan","fingerprint","use-auth-secret",`static-auth-secret=${secret}`,
  "cert=/secrets/server.crt","pkey=/secrets/server.key","no-cli","no-multicast-peers","no-dtls","no-tlsv1","no-tlsv1_1","no-tcp-relay","user-quota=12","total-quota=32",
  "denied-peer-ip=0.0.0.0-255.255.255.255","denied-peer-ip=::-ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",`allowed-peer-ip=${subnet}.0-${subnet}.255`,"log-file=stdout","simple-log","pidfile=/tmp/turnserver.pid",
].join("\n")+"\n",{mode:0o600});
for(const name of ["ca.key","server.key"]) fs.chmodSync(`${directory}/${name}`,0o600);
console.log(`LAN certificates generated for ${primary} and ${alternate}; TURN peer ACL ${subnet}.0/24.`);
