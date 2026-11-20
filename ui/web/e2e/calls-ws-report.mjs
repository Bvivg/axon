import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";

const directory = process.env.CALLS_EVIDENCE_DIR ?? "/evidence";
const read = name => JSON.parse(fs.readFileSync(path.join(directory, name), "utf8"));
const playwright = read("playwright-results.json");
const stats = playwright.stats;
if (stats.expected !== 5 || stats.unexpected || stats.flaky || stats.skipped) throw new Error(`Gate is not green: ${JSON.stringify(stats)}`);
const database = fs.readFileSync(path.join(directory, "database-final.jsonl"), "utf8").trim().split("\n").map(line => JSON.parse(line));
const cases = ["direct-audio", "relay-tls-video"].map(name => read(name + ".json"));
const controls = Object.fromEntries(["resync", "revocation", "redis-recovery"].map(name => [name, read(name + ".json")]));
if (Object.values(controls).some(value => !value.passed)) throw new Error("A control case did not pass");
const media = cases.map(value => {
  if (!value.passed || value.mockMedia || value.slotsBeforeEnd !== 2 || value.slotsAfterEnd !== 0) throw new Error(`Incomplete media case ${value.route}`);
  const terminal = database.find(row => row.call.id === value.callID);
  if (!terminal || terminal.call.state !== "ended" || terminal.slots !== 0 || terminal.call.participants.some(p => !p.cleanup_done)) throw new Error(`Missing terminal DB proof ${value.callID}`);
  const endpoints = value.after.map((after, index) => {
    const before = value.before[index], clean = value.cleanup[index];
    const relay = value.route.startsWith("relay");
    if (!after.selected || (after.selected.local.candidateType === "relay") !== relay || (after.selected.remote.candidateType === "relay") !== relay) throw new Error("Wrong selected ICE route");
    const delta = {
      audioInBytes: after.audioIn.bytes - before.audioIn.bytes,
      audioOutBytes: after.audioOut.bytes - before.audioOut.bytes,
      audioInPackets: after.audioIn.packets - before.audioIn.packets,
      audioOutPackets: after.audioOut.packets - before.audioOut.packets,
      receivedAudioEnergy: after.measuredAudio.energy - before.measuredAudio.energy,
      receivedAudioSamples: after.measuredAudio.samples - before.measuredAudio.samples,
      videoInFrames: after.videoIn.frames - before.videoIn.frames,
      videoOutFrames: after.videoOut.frames - before.videoOut.frames,
    };
    if (delta.audioInBytes <= 0 || delta.audioOutBytes <= 0 || delta.audioInPackets <= 0 || delta.receivedAudioEnergy <= 0 || (value.route.includes("video") && delta.videoInFrames <= 0)) throw new Error("RTP/audio did not grow");
    if (clean.connectionState !== "closed" || clean.audioContextState !== "closed" || clean.mediaElements !== 0 || clean.tracks.some(track => track.readyState !== "ended")) throw new Error("Incomplete native cleanup");
    return { pair: after.selected.local.candidateType + "/" + after.selected.remote.candidateType, selectedBy: after.selected.basis, relayProtocol: after.selected.local.relayProtocol ?? null, delta, cleanup: clean };
  });
  return { route: value.route, browser: value.engine, callID: value.callID, roomID: value.room, active: value.call, terminal: terminal.call, slots: [2, 0], outboxRows: terminal.outbox, endpoints, history: value.history, legacyFrames: value.legacyFrames };
});
const tracks = media.flatMap(value => value.endpoints.flatMap(endpoint => endpoint.cleanup.tracks));
const result = {
  gate: "R1-1.2-1.3", timestamp: new Date().toISOString(),
  tests: { passed: stats.expected, failed: stats.unexpected, flaky: stats.flaky, skipped: stats.skipped, retries: 0, testTimeoutMs: 30000, expectTimeoutMs: 10000, durationMs: stats.duration },
  signaling: cases[0].signaling, mockMedia: false, media, controls,
  endedTrackReferences: tracks.length,
  push: "не проверен", externalNAT: "внешние NAT не проверены",
  livekitRoom: "N/A: LiveKit/SFU не разрешён в Рубеже 1",
  limitations: ["Chromium pairs only in 1.3; Firefox native media covered separately in 1.1", "Media lifecycle is exercised by a gated diagnostic consumer; CallManager and feature UI are step 1.4", "Full page reload/tab ownership persistence, hardware, Safari/iOS and LAN are not verified", "Push delivery is not implemented for Calls yet", "No mid-media Chat process crash or physical network interruption exercised"],
  stop: "Обязательная остановка после 1.3 до UI; Рубежи 2/3 не разрешены",
};
fs.writeFileSync(path.join(directory, "result.json"), JSON.stringify(result, null, 2));
const lines = [
  "# Evidence — Рубеж 1, шаги 1.2–1.3", "",
  "5/5 browser tests PASS. Рубеж 1 целиком не закрыт; обязательная остановка до feature UI (1.4).", "",
  "Настоящие Register/login/refresh/direct chat, gateway и Chat shipping containers, PostgreSQL/outbox, UserSignalBus/Redis, существующий activity WS и ChatActivityProvider CallChannel. Native RTCPeerConnection, настоящий coturn TURN REST/TLS, TLS validation включена. Диагностический consumer доступен только при CALLS_E2E_PROBE=1; SDP/ICE пересылается через Chat WS, Node manual SDP bridge не используется. Browser fake capture devices дают тестовый источник для настоящих WebRTC и audio output.", "",
  "| Сценарий | Selected pair endpoints | Δ audio RX bytes | Δ audio energy | Δ video RX frames | DB slots |", "|---|---|---|---|---|---|",
  ...media.map(row => `| ${row.route} | ${row.endpoints.map(e => e.pair).join(", ")} | ${row.endpoints.map(e => e.delta.audioInBytes).join(", ")} | ${row.endpoints.map(e => e.delta.receivedAudioEnergy.toFixed(3)).join(", ")} | ${row.endpoints.map(e => e.delta.videoInFrames).join(", ")} | 2 → 0 |`),
  "", `End: все ${tracks.length} сохранённых local/remote track refs readyState=ended; PC/AudioContext=closed, media elements=0. После Cleanup ACK реальная БД подтверждает ENDED и cleanup_done=true у обоих участников, slots=0. ACTIVE Call/room/participants и terminal DB documents сохранены в result.json; история возвращает завершённый Call ID. LiveKit room=N/A: SFU не входит в этот рубеж.`, "",
  "Контрольные проверки: unacked event повторно приходит после WS reconnect/epoch1→2; старый epoch отклоняется failed_precondition. Настоящий Auth.Logout закрывает именно активный activity socket, старый JWT не может RegisterEndpoint/StartCall/GetMediaCredentials (403). Реальный stop/start только Redis изолированного Calls-стенда: два события commit в PostgreSQL во время outage, ни одного события клиентам до восстановления, затем доставка обоим и slots=0. Legacy Chat socket получает subscribed/ack/message и ни одного call_event.", "",
  "Go race unit/integration: PASS, настоящие Postgres/Redis. Проверены request replay/fingerprint, outsider ACL, crossed starts, concurrent endpoint accept, per-endpoint ACK, abandoned outbox claim, connection/family/epoch fencing, expiry vs lease renewal. Gateway catalogue tests: PASS. Go lint/Buf lint/TypeScript/targeted ESLint: PASS. SQL up/down/up в отдельной транзакционной схеме:6→0→6 таблиц, ROLLBACK удаляет probe schema. Миграция13 на основном dev не применялась, Calls выключен по default.", "",
  "Не проверено: full-page reload/tab-clone ownership, hardware/Safari/iOS, LAN, разрыв процесса Chat во время настоящего медиазвонка. CallManager, кнопки, overlay, device controls и push — следующий шаг1.4. Нет Call UI, SFU и migration кода. В1.3 проверен Chromium156.0.8078.4; Firefox157 native media отдельно в evidence1.1.", "",
  "**push: не проверен. внешние NAT не проверены.**", "",
  "Вывод команд: browser-final.log и checks/*.log; COMMANDS.txt содержит точные команды и exit codes. Полные native snapshots: direct-audio.json и relay-tls-video.json. Контрольные случаи: resync.json, revocation.json, redis-recovery.json. SQL terminal состояния: database-final.jsonl. Image IDs: images.txt. SHA256: manifest.json. .secrets и браузерные профили не публиковать.", "",
  result.stop + ".",
];
fs.writeFileSync(path.join(directory, "README.md"), lines.join("\n") + "\n");
const sourceManifest = {};
const sourceRoots = [
  "core/shared/proto/axon/calls", "core/services/chat/internal/calls",
  "core/services/chat/internal/repository/calls.go", "core/services/chat/internal/repository/calls_jobs.go", "core/services/chat/internal/repository/calls_binding.go",
  "core/services/chat/internal/pubsub/pubsub.go", "core/services/chat/internal/pubsub/redis.go", "core/services/chat/internal/pubsub/calls.go",
  "core/services/chat/internal/ws/calls.go", "core/services/chat/internal/ws/handler.go", "core/services/chat/internal/ws/protocol.go",
  "core/services/chat/internal/server/calls.go", "core/services/chat/internal/config/config.go", "core/services/chat/internal/identity/identity.go", "core/services/chat/cmd/chat/main.go",
  "core/services/chat/integration/calls_test.go", "core/services/chat/migrations/000013_calls.up.sql", "core/services/chat/migrations/000013_calls.down.sql",
  "core/services/gateway/internal/proxy/calls.go", "core/services/gateway/internal/policy/policy.go", "core/services/gateway/cmd/gateway/main.go",
  "core/deploy/docker-compose.calls-e2e.yml", "core/deploy/docker-compose.calls-test.yml", "core/deploy/Dockerfile.calls-test", "core/deploy/calls-spike-run.sh",
  "ui/web/src/lib/calls", "ui/web/src/components/calls", "ui/web/src/app/(app)/calls/probe",
  "ui/web/src/lib/ws/use-room-activity-socket.ts", "ui/web/src/components/chat/chat-activity.tsx", "ui/web/src/lib/connect/clients.ts",
  "ui/web/e2e/calls-ws.spec.ts", "ui/web/e2e/calls-ws-support.ts", "ui/web/e2e/calls-ws-report.mjs", "ui/web/e2e/calls-spike-support.ts", "ui/web/e2e/calls-spike-init.mjs",
  "ui/web/playwright.calls-ws.config.ts", "ui/web/playwright.calls-spike.config.ts", "ui/web/playwright.config.ts",
];
function hashSource(filename) {
  if (fs.statSync(filename).isDirectory()) for (const entry of fs.readdirSync(filename)) hashSource(path.join(filename, entry));
  else sourceManifest[filename] = crypto.createHash("sha256").update(fs.readFileSync(filename)).digest("hex");
}
for (const filename of sourceRoots) hashSource(filename);
fs.writeFileSync(path.join(directory, "source-manifest.json"), JSON.stringify(sourceManifest, null, 2));
const manifest = {};
function hashTree(root) {
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    if (entry.name.startsWith(".") || ["test-results", "manifest.json", "report-output.log"].includes(entry.name)) continue;
    const filename = path.join(root, entry.name);
    if (entry.isDirectory()) hashTree(filename);
    else manifest[path.relative(directory, filename)] = crypto.createHash("sha256").update(fs.readFileSync(filename)).digest("hex");
  }
}
hashTree(directory);
fs.writeFileSync(path.join(directory, "manifest.json"), JSON.stringify(manifest, null, 2));
console.log(JSON.stringify({ gate: result.gate, tests: result.tests, endedTrackReferences: tracks.length, media: media.map(row => ({ route: row.route, endpoints: row.endpoints.map(e => ({ pair: e.pair, delta: e.delta })), slots: row.slots })) }, null, 2));
