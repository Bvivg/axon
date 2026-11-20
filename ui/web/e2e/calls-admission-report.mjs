import fs from "node:fs";
import path from "node:path";
import {createHash} from "node:crypto";
import assert from "node:assert/strict";

const root="/evidence";
const read=name=>JSON.parse(fs.readFileSync(path.join(root,name),"utf8"));
const native=read("admission-native.json"),deny=read("admission-deny.json"),late=read("admission-late-join.json"),refresh=read("admission-refresh.json"),tests=read("playwright-results.json");
const sql=read("final-sql.json");assert.equal(sql.slots,0);assert.equal(sql.nonterminal_calls,0);
assert.equal(tests.stats.expected,3);assert.equal(tests.stats.unexpected,0);assert.equal(refresh.passed,true);
assert.equal(deny.matrix.length,36);assert.equal(deny.serverApi,404);assert.equal(deny.privateRpc,404);
assert.equal(refresh.denied.length,8);assert.equal(native.slots,0);
assert.ok(late.status.opened && late.status.messages>0 && late.status.closed);
const metrics=native.media.map((snapshot,index)=>{
 const rows=snapshot.reports.flatMap(r=>r.rows),old=native.initial[index].reports.flatMap(r=>r.rows);
 const audio=rows.find(r=>r.type==="inbound-rtp"&&r.kind==="audio"),before=old.find(r=>r.type==="inbound-rtp"&&r.kind==="audio");
 const video=rows.find(r=>r.type==="inbound-rtp"&&r.kind==="video"),oldVideo=old.find(r=>r.type==="inbound-rtp"&&r.kind==="video");
 const transport=rows.find(r=>r.type==="transport"&&r.selectedCandidatePairId);
 const pair=rows.find(r=>r.id===transport.selectedCandidatePairId);
 const local=rows.find(r=>r.id===pair.localCandidateId),remote=rows.find(r=>r.id===pair.remoteCandidateId);
 assert.equal(pair.state,"succeeded");
 const output=snapshot.playback.find(p=>p.kind==="AUDIO");assert.ok(output&&!output.paused&&!output.muted);
 const result={audioBytesDelta:audio.bytesReceived-(before?.bytesReceived??0),audioPacketsDelta:audio.packetsReceived-(before?.packetsReceived??0),audioEnergyDelta:audio.totalAudioEnergy-(before?.totalAudioEnergy??0),videoFramesDelta:video.framesDecoded-(oldVideo?.framesDecoded??0),selectedPair:{id:pair.id,local:local.candidateType,remote:remote.candidateType,protocol:local.protocol},playback:output};
 assert.ok(result.audioBytesDelta>0&&result.audioPacketsDelta>0&&result.audioEnergyDelta>0&&result.videoFramesDelta>0);
 return result;
});
const cleanup=[...native.cleanup,...refresh.cleanup];
for(const item of cleanup){assert.equal(item.state,"disconnected");assert.ok(item.tracks.length>=4&&item.tracks.every(t=>t.readyState==="ended"));assert.equal(item.elements,0);}
const result={gate:"R2-admission",passed:true,versions:{server:"v1.13.11",js:"2.23.0",go:"v2.19.0"},browser:{passed:tests.stats.expected,unexpected:tests.stats.unexpected},matrix:deny.matrix.length,refreshMs:refresh.elapsedMs,reconnect:refresh.reconnect.map(r=>({scenario:r.scenario,elapsedMs:r.elapsedMs,websocket:r.websocket})),revokeRemovalMs:refresh.revokeRemovalMs,lateJoinRemovalObservedMs:late.removalObservedMs,metrics,endedTrackRefs:cleanup.reduce((sum,c)=>sum+c.tracks.length,0),slots:0,limitations:["Measured removal times are observations, not hard upper bounds; Server API outage has no guaranteed removal deadline","Probe uses real R1 fixtures; group CRUD/admission and transport-kind gating belong to2.2","push: не проверен","внешние NAT не проверены","Safari/iOS physical devices unverified"]};
fs.writeFileSync(path.join(root,"result.json"),JSON.stringify(result,null,2));
fs.writeFileSync(path.join(root,"README.md"),`# Рубеж2.1 — admission spike\n\nLiveKit v1.13.11 / JS2.23.0 / GoSDK2.19.0. Docker native Chromium; 3/3 E2E PASS, timeout30s/expect10s/retries0. Исходный и refreshed JWT ограждены актуальными SQL/Auth checks, private API публично недоступен.\n\n- Native v0/v1 WS join, validate200 для обоих протоколов, selected host/host ICE, двусторонние RTP/audio energy/video frames и фактическое audio playback.\n- Real-clock refresh + новые WS и рост RTP после signal/full reconnect: ${refresh.elapsedMs}ms; refreshed exp/identity сохраняют binding; после Logout original/refreshed JWT получают403 на всех4routes.\n- Revoke→participant removal: ${refresh.revokeRemovalMs}ms. Настоящий TCP drop задержал уже допущенный join до End; пришедший позже participant удалён, наблюдение ${late.removalObservedMs}ms. Это измерения, не обещание нулевого окна или SLA при недоступном Server API.\n- ${result.endedTrackRefs} сохранённых native track refs ended, rooms disconnected, media elements0; основной media-case SQLslots0 и room removed.\n- Go internal/regression, Buflint, golangci-lint0issues, TypeScript/targetESLint,8realPostgres/Redis integration PASS.\n\nЭто только2.1: group/fallback/replacement ещё не реализованы, Рубеж2 в целом не закрыт. Probe включён только на isolated fixtures через CALLS_MEDIA_ADMISSION_PROBE, обычные P2P callers не получают SFU credential capability. Push/внешниеNAT/hardwareSafari не проверены.\n\nСм. result.json, raw snapshots, COMMANDS.txt, build/browser/refresh/integration/lint/check logs. .secrets и test traces не публиковать.\n`);
const manifest=[];
for(const name of fs.readdirSync(root)){const file=path.join(root,name);if(!fs.statSync(file).isFile()||name==="manifest.json")continue;manifest.push({file:name,sha256:createHash("sha256").update(fs.readFileSync(file)).digest("hex")});}
fs.writeFileSync(path.join(root,"manifest.json"),JSON.stringify(manifest,null,2));
console.log(JSON.stringify({passed:result.passed,browser:result.browser,endedTrackRefs:result.endedTrackRefs,metrics:result.metrics,revokeRemovalMs:result.revokeRemovalMs,lateJoinRemovalObservedMs:result.lateJoinRemovalObservedMs}));
