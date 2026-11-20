import fs from "node:fs";
import path from "node:path";

const directory = process.env.CALLS_EVIDENCE_DIR || "/evidence";
const read = name => JSON.parse(fs.readFileSync(path.join(directory, name), "utf8"));
const need = (condition, message) => { if (!condition) throw new Error(message); };
const tests = read("playwright-sfu-results.json");
need(tests.stats.expected === 6 && tests.stats.unexpected === 0 && tests.stats.skipped === 0 && tests.stats.flaky === 0, "Six native SFU tests must pass without retries or skips");
const names = ["replacement", "group", "fallback", "revoke", "reinvite-caller"];
const media = names.map(name => {
  const row = read("sfu-" + name + ".json");
  need(row.nativeMedia && row.media && row.cleanup, "Missing native proof: " + name);
  const deltas = row.media.after.map((after, i) => {
    const before = row.media.before[i];
    const delta = { audioBytes: after.audioBytes - before.audioBytes, audioPackets: after.audioPackets - before.audioPackets, energy: after.energy - before.energy, frames: after.frames - before.frames };
    need(delta.audioBytes > 0 && delta.audioPackets > 0 && delta.energy > 0, "No audio growth: " + name);
    if (name === "replacement") need(delta.frames > 0, "No video growth");
    need(after.selected.length > 0 && after.playback.every(output => !output.paused && !output.muted && output.time > 0), "Missing selected ICE/playback");
    return { ...delta, selected: after.selected.map(pair => ({ local: pair.local.candidateType, remote: pair.remote.candidateType, protocol: pair.local.protocol })) };
  });
  const ended = row.cleanup.reduce((n, state) => {
    need(state.state === "disconnected" && state.tracks.every(track => track.state === "ended"), "Incomplete native cleanup: " + name);
    return n + state.tracks.length;
  }, 0);
  return { name, participants: row.media.server.participants.length, deltas, ended };
});
const busy = read("sfu-busy.json");
need(busy.passed && busy.after.every((value, i) => value > busy.before[i]), "Busy interrupted original media");
const refresh = read("sfu-refresh.json");
need(refresh.passed && refresh.refresh.length && refresh.reconnect.length === 2 && refresh.denied.length === 4, "Missing real refresh/reconnect/revoke");
need(refresh.cleanup.every(state => state.state === "disconnected" && state.tracks.every(track => track.readyState === "ended")), "Refresh cleanup incomplete");
const lan = read("lan-sfu.json");
need(lan.passed && lan.server.participants.length === 3, "Main LAN group not proven");
const lanDeltas = lan.after.map((after, i) => {
  const before = lan.before[i];
  const delta = { audioBytes: after.bytes - before.bytes, energy: after.energy - before.energy, frames: after.frames - before.frames };
  need(delta.audioBytes > 1000 && delta.energy > 0.0001 && delta.frames > 0, "LAN audio/video proof too weak");
  return { ...delta, selected: after.selected.map(pair => ({ local: pair.local.candidateType, remote: pair.remote.candidateType, protocol: pair.local.protocol })) };
});
need(lan.cleanup.every(state => state.state === "disconnected" && state.tracks.every(track => track.readyState === "ended") && state.pcs.every(pc => pc === "closed")), "LAN cleanup incomplete");
const result = { passed: true, nativeTests: 6, media, busyPreservesOriginal: true, refresh: { elapsedMs: refresh.elapsedMs, identities: refresh.refresh, reconnect: refresh.reconnect.map(item => ({ scenario: item.scenario, elapsedMs: item.elapsedMs, bytes: item.after.audioBytes - item.baseline.audioBytes, energy: item.after.energy - item.baseline.energy, frames: item.after.frames - item.baseline.frames })), observedRevokeRemovalMs: refresh.revokeRemovalMs, denied: refresh.denied, ended: refresh.cleanup.reduce((n, item) => n + item.tracks.length, 0) }, lan: { origin: lan.origin, participants: 3, deltas: lanDeltas, ended: lan.cleanup.reduce((n, item) => n + item.tracks.length, 0) }, limits: ["push: не проверен", "внешние NAT не проверены", "Safari/iOS physical hardware: не проверен", "revoke/removal timings are observations, not hard bounds during Server API outage"] };
fs.writeFileSync(path.join(directory, "result.json"), JSON.stringify(result, null, 2));
console.log(JSON.stringify({ passed: true, nativeTests: result.nativeTests, refreshMs: refresh.elapsedMs, observedRevokeRemovalMs: refresh.revokeRemovalMs, lan: result.lan }));
