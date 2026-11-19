import fs from "node:fs";

const directory = process.env.CALLS_EVIDENCE_DIR ?? "/evidence";
const playwright = JSON.parse(fs.readFileSync(`${directory}/playwright-results.json`, "utf8"));
const stats = playwright.stats;
if (stats.expected !== 21 || stats.unexpected || stats.flaky || stats.skipped) throw new Error(`Full gate is not green: ${JSON.stringify(stats)}`);
const cases = fs.readdirSync(`${directory}/cases`).filter(name => name.endsWith(".json")).map(name => JSON.parse(fs.readFileSync(`${directory}/cases/${name}`, "utf8")));
const media = cases.filter(value => value.result.passed && value.result.before && value.result.after);
if (media.length !== 16) throw new Error(`Expected 16 complete native media cases, received ${media.length}`);
const rows = media.map(value => ({
  browser: value.browser,
  version: value.result.engineVersion,
  mode: value.result.video ? "video" : "audio",
  route: value.result.requestedRoute,
  endpoints: value.result.after.map((after, index) => {
    const before = value.result.before[index];
    return {
      pair: `${after.selected.local.candidateType}/${after.selected.remote.candidateType}`,
      selectedBy: after.selected.basis,
      relayProtocol: after.selected.local.relayProtocol ?? null,
      audioInBytesDelta: after.audioIn.bytes - before.audioIn.bytes,
      audioOutBytesDelta: after.audioOut.bytes - before.audioOut.bytes,
      audioInPacketsDelta: after.audioIn.packets - before.audioIn.packets,
      audioEnergyDelta: after.measuredAudio.energy - before.measuredAudio.energy,
      nativeAudioEnergyDelta: after.audioIn.energy - before.audioIn.energy,
      measuredSamplesDelta: after.measuredAudio.samples - before.measuredAudio.samples,
      videoInFramesDelta: after.videoIn.frames - before.videoIn.frames,
      videoOutFramesDelta: after.videoOut.frames - before.videoOut.frames,
      allTracksEnded: value.result.cleanup[index].tracks.every(track => track.readyState === "ended"),
      connectionStateAfterEnd: value.result.cleanup[index].connectionState,
    };
  }),
}));
const cleanup = cases.flatMap(value => value.result.cleanup ?? []);
const trackReferences = cleanup.flatMap(value => value.tracks ?? []);
if (!trackReferences.every(value => value.readyState === "ended")) throw new Error("A captured track is still live");
const report = {
  gate: "R1-spike-1.1",
  timestamp: new Date().toISOString(),
  tests: { passed: stats.expected, failed: stats.unexpected, retries: 0, timeoutMs: 30000, expectTimeoutMs: 10000 },
  media: rows,
  endedTrackReferences: trackReferences.length,
  room: "N/A: business backend not implemented",
  participants: "two real browser endpoints per test",
  slots: "N/A: Calls tables not implemented; no slots reserved",
  push: "не проверен",
  externalNAT: "внешние NAT не проверены",
  signaling: "Diagnostic harness only; Chat WS delivery not verified",
  limitations: ["Peer pairs use the same browser engine; Chromium/Firefox interoperability not exercised", "Permission denial tested in Chromium; unavailable device tested in both engines", "No physical devices, external NAT or push delivery tested"],
};
fs.writeFileSync(`${directory}/result.json`, JSON.stringify(report, null, 2));
const lines = [
  "# Evidence — Рубеж 1, spike 1.1", "", `${stats.expected}/21 PASS. Рубеж 1 целиком не закрыт.`, "",
  "Native RTCPeerConnection/getUserMedia, Chromium и Firefox, без mock медиа. Origin: http://localhost:3000, TLS validation включена; тестовый CA импортирован в disposable stores. TURN TLS 5349 проверен с блокировкой UDP к 3478. Обычная bridge-сеть с default route нужна Firefox ICE; TURN peer ACL ограничен тестовой подсетью, host ports не опубликованы. Firefox Web Audio использует настоящий PulseAudio null sink в контейнере.", "",
  "| Browser | Media | Route | Selected pair endpoints | Δ audio received bytes endpoints | Δ audio energy endpoints | Δ video decoded frames endpoints |", "|---|---|---|---|---|---|---|",
  ...rows.map(row => `| ${row.browser} ${row.version} | ${row.mode} | ${row.route} | ${row.endpoints.map(e => e.pair).join(", ")} | ${row.endpoints.map(e => e.audioInBytesDelta).join(", ")} | ${row.endpoints.map(e => e.audioEnergyDelta.toFixed(3)).join(", ")} | ${row.endpoints.map(e => e.videoInFramesDelta).join(", ")} |`),
  "", `После End все ${trackReferences.length} сохранённых track refs: readyState=ended, PC=closed, AudioContext=closed, media elements удалены. Во всех 16 positive cases повторный capture live → ended.`, "",
  "Отрицательные проверки: в обоих движках заблокированный TURN UDP → нет выбранной пары/RTP до реального setup deadline → cleanup; native NotAllowedError при запрете microphone permission в Chromium; native ошибка при выборе отсутствующего device ID в обоих движках. Физическое отсутствие микрофона не проверялось.", "",
  "Пары в этом spike используют один движок: Chromium↔Chromium и Firefox↔Firefox. Interop Chromium↔Firefox пока не проверен.", "",
  "Room/DB participants/slots: N/A — backend Calls не написан, slots не резервировались. SDP/ICE обменивает только диагностический harness; delivery через Chat/Redis/WS не доказана.", "",
  "push: не проверен. внешние NAT не проверены. Safari/iOS hardware: не проверено.", "",
  "Команды и exit codes: COMMANDS.txt. Полные stats: result.json и cases/*.json. Лог: final-run.log. Coturn allocations/control protocols: coturn.log. Secrets из .secrets не включать в git или публикуемый отчёт.", "",
  "Обязательная остановка после 1.1. Шаг 1.2 не начат; ожидается подтверждение владельца.",
];
fs.writeFileSync(`${directory}/README.md`, lines.join("\n") + "\n");
console.log(JSON.stringify({gate:report.gate,passed:stats.expected,mediaCases:rows.length,endedTrackReferences:trackReferences.length}));
