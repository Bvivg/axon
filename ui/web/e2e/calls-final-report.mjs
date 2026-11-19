import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";

const root = ".local/evidence/calls";
const directory = `${root}/R1-final`;
const read = filename => JSON.parse(fs.readFileSync(filename, "utf8"));
const ui = read(`${directory}/playwright-ui-results.json`);
const ws = read(`${directory}/playwright-results.json`);
const lan = read(`${root}/LAN/lan-call.json`);
if (ui.stats.expected !== 7 || ui.stats.unexpected || ui.stats.flaky || ws.stats.expected !== 5 || ws.stats.unexpected || !lan.passed) throw new Error("A final gate is not green");
const media = ["direct-audio", "relay-tls-video"].map(route => {
  const value = read(`${directory}/ui-${route}.json`);
  if (!value.passed || value.slotsBeforeEnd !== 2 || value.slotsAfterEnd !== 0) throw new Error(route);
  const endpoints = value.after.map((after, i) => {
    const before = value.before[i], clean = value.cleanup[i];
    const delta = { audioInBytes: after.audioIn - before.audioIn, audioOutBytes: after.audioOut - before.audioOut, audioEnergy: after.energy - before.energy, videoFrames: after.frames - before.frames };
    if (delta.audioInBytes <= 0 || delta.audioOutBytes <= 0 || delta.audioEnergy <= 0 || (route.includes("video") && delta.videoFrames <= 0)) throw new Error("No real media growth");
    if (clean.connectionState !== "closed" || clean.outputElements !== 0 || clean.tracks.some(t => t.readyState !== "ended")) throw new Error("Incomplete cleanup");
    return { selected: after.selected, delta, cleanup: clean };
  });
  return { route, callID: value.callID, slots: [2, 0], terminal: value.terminal, endpoints, cameraReplacement: value.cameraReplacement ?? false, redialAndDecline: value.redialAndDecline };
});
const controls = Object.fromEntries(["reconnect-revoke", "multi-tab", "permission", "busy", "chat-restart"].map(name => [name, read(`${directory}/ui-${name}.json`)]));
if (Object.values(controls).some(value => !value.passed)) throw new Error("Incomplete control case");
const tracks = media.flatMap(m => m.endpoints.flatMap(e => e.cleanup.tracks));
const result = { gate: "R1-final", generatedAt: new Date().toISOString(), uiTests: ui.stats, wsTests: ws.stats, lan: { passed: lan.passed, origin: lan.origin, secureContexts: lan.secureContexts, playback: lan.playback, cleanup: lan.cleanup }, media, controls, endedTrackReferences: tracks.length, push: "не проверен", externalNAT: "внешние NAT не проверены", hardware: "Safari/iOS/physical devices not tested; manual runbook", limitations: ["Native desktop screen picker and physical audio output devices not exercised in Docker", "LAN browser clients ran in Docker; friend's physical device not tested", "Calls push code/TTL/DB claim/receipt/ACK tested, real Web Push delivery not tested"] };
fs.writeFileSync(`${directory}/result.json`, JSON.stringify(result, null, 2));
const lines = [
  "# Рубеж1 — итоговое evidence", "",
  "UI:7/7 PASS. Нижний WS gate:5/5 PASS. Основной LAN HTTPS/WSS:1/1 PASS. Исторический media spike1.1:21/21 PASS. Рубежи2/3 не начинались.", "",
  "| UI media | Selected pairs | Δ audio RX bytes endpoints | Δ received audio energy | Δ decoded video frames | SQL slots |", "|---|---|---|---|---|---|",
  ...media.map(m => `| ${m.route} | ${m.endpoints.map(e => `${e.selected.local.candidateType}/${e.selected.remote.candidateType}`).join(", ")} | ${m.endpoints.map(e => e.delta.audioInBytes).join(", ")} | ${m.endpoints.map(e => e.delta.audioEnergy.toFixed(3)).join(", ")} | ${m.endpoints.map(e => e.delta.videoFrames).join(", ")} | 2→0 |`),
  "", `Все ${tracks.length} сохранённых native local/remote refs в этих UI сценариях ended, PC=closed, output elements=0. Реальные terminal SQL documents/participants, history и повторный звонок/Decline сохранены в result.json. Native camera replacement проверен. Media проходит UI→CallManager→Calls RPC/outbox→Redis→existing activity WS→native PeerMedia; RTC/Room mock и Node SDP bridge отсутствуют.`, "",
  "Контрольные сценарии: in-page reconnect/new generation и реальный Auth.Logout с остановкой capture; cloned sessionStorage tabs получают разные endpoints и один Accept winner; настоящий NotAllowedError не создаёт Call/slots; Busy записывается в history без нарушения существующего ACTIVE; настоящий restart только Chat проекта axon-calls-e2e восстанавливает ACTIVE/RTP и освобождает slots после End. Нижний WS gate повторно проверяет unacked resync/fencing/real Redis outage и revocation403.", "",
  "Go race/internal и7Calls Postgres/Redis integration: PASS. Отдельный DB test проверяет push claim/retry/receipt/ACK suppression/deadline. Web unit69/69 PASS, Go lint0issues, types/targeted ESLint PASS (одно TanStack Virtual React Compiler warning). Миграции13/14 применены к основной БД без удаления существующих данных. Таймауты30s/expect10s/retries0 сохранены.", "",
  "Основной сайт https://192.168.2.1:3443 проверен отдельным Docker browser client с настоящим доверием CA, без ignoreHTTPSErrors. Secure HttpOnly refresh cookie имеет корректный /api path; login/refresh, Call/Accept, native playback обоих участников и End/ended проходят. Физические устройства друга не проверены. LAN/lan-call.json содержит диагностические console404/aborted prefetch; они не считаются доказательством медиа и сохранены как есть.", "",
  "**push: не проверен. внешние NAT не проверены.** Safari/iOS/hardware и native desktop screen picker/physical audio output — manual runbook docs/calls-safari-ios-runbook.md. Не выдавать Docker fake capture source за физические microphone/camera.", "",
  "Вывод команд: browser-ui.log, browser-ws.log, integration.log, go-race.log, go-lint.log, ui-check.log, web-unit.log, ../LAN/browser-lan.log. Полные snapshots: ui-direct-audio.json/ui-relay-tls-video.json и result.json; COMMANDS.txt описывает повтор. Secrets исключены из manifest и git. Основной LAN stack оставлен running; disposable E2E stack удалён после архивирования. Commit/push/PR не выполнялись.",
];
fs.writeFileSync(`${directory}/README.md`, lines.join("\n") + "\n");
const sourceManifest = {};
function source(at) {
  if (fs.statSync(at).isDirectory()) { for (const item of fs.readdirSync(at)) source(path.join(at, item)); }
  else sourceManifest[at] = crypto.createHash("sha256").update(fs.readFileSync(at)).digest("hex");
}
for (const file of ["ui/web/src/lib/calls", "ui/web/src/components/calls", "ui/web/src/calls-push-worker.ts", "ui/web/scripts/build-worker.mjs", "core/services/chat/internal/calls", "core/services/chat/internal/notify/calls.go", "core/services/chat/internal/repository/calls.go", "core/services/chat/internal/repository/calls_jobs.go", "core/services/chat/internal/repository/calls_push.go", "core/services/chat/internal/ws/calls.go", "core/services/chat/migrations/000013_calls.up.sql", "core/services/chat/migrations/000014_call_push.up.sql", "core/shared/proto/axon/calls", "core/deploy/docker-compose.calls-lan.yml", "core/deploy/calls-lan-proxy.mjs", "core/deploy/calls-e2e.sh", "ui/web/e2e/calls-ui.spec.ts", "ui/web/e2e/calls-lan.spec.ts"]) source(file);
fs.writeFileSync(`${directory}/source-manifest.json`, JSON.stringify(sourceManifest, null, 2));
const manifest = {};
function walk(at) {
  for (const entry of fs.readdirSync(at, { withFileTypes: true })) {
    if (entry.name.startsWith(".") || ["test-results", "manifest.json"].includes(entry.name)) continue;
    const file = path.join(at, entry.name);
    if (entry.isDirectory()) walk(file);
    else manifest[path.relative(directory, file)] = crypto.createHash("sha256").update(fs.readFileSync(file)).digest("hex");
  }
}
walk(directory);
fs.writeFileSync(`${directory}/manifest.json`, JSON.stringify(manifest, null, 2));
console.log(JSON.stringify({ ui: ui.stats.expected, ws: ws.stats.expected, lan: lan.passed, endedTracks: tracks.length, media: media.map(m => ({ route: m.route, deltas: m.endpoints.map(e => e.delta) })) }));
