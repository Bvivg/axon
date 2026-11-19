import fs from "node:fs";
import { createHmac,randomUUID } from "node:crypto";
import { expect,type Browser,type Page } from "@playwright/test";
import { callPeers,callState,callSQL } from "./calls-ws-support";

export const credentials=JSON.parse(fs.readFileSync("/evidence/.secrets/admission.json","utf8")) as {key:string;secret:string;internal:string};
export function token(identity:string,room:string,extra:Record<string,unknown>={},secret=credentials.secret){
 const now=Math.floor(Date.now()/1000);
 const head=Buffer.from(JSON.stringify({alg:"HS256",typ:"JWT"})).toString("base64url");
 const claims=Buffer.from(JSON.stringify({iss:credentials.key,sub:identity,nbf:now-1,exp:now+120,video:{roomJoin:true,room,canPublish:true,canSubscribe:true,canPublishData:false},...extra})).toString("base64url");
 const body=`${head}.${claims}`;
 return `${body}.${createHmac("sha256",secret).update(body).digest("base64url")}`;
}
export async function admin(page:Page,method:string,data:object,grant:Record<string,unknown>){
 const response=await page.request.post(`http://livekit:7880/twirp/livekit.RoomService/${method}`,{headers:{Authorization:`Bearer ${token("admission-probe","",{video:grant})}`},data});
 expect(response.ok()).toBe(true);
 return response.json();
}
export async function admissionPeers(browser:Browser){
 const peers=await callPeers(browser);
 try{
  const pages=[peers.a.page,peers.b.page];
  for(const page of pages){
   await page.goto("http://localhost:3000/calls/spike");
   await expect.poll(()=>page.evaluate(()=>!!window.axonLiveKitProbe && !!window.axonCallsProbe?.channel.endpoint)).toBe(true);
  }
  const id=await peers.a.page.evaluate(async roomId=>{const api=window.axonCallsProbe!;return (await api.rpc.startCall({endpoint:api.channel.endpoint,requestId:crypto.randomUUID(),roomId,mode:2})).call!.id;},peers.roomID);
  await expect.poll(()=>callState(peers.b.page,id)).toBe(1);
  await peers.b.page.evaluate(async callId=>{const api=window.axonCallsProbe!;await api.rpc.acceptCall({endpoint:api.channel.endpoint,requestId:crypto.randomUUID(),callId});},id);
  for(const page of pages)await expect.poll(()=>callState(page,id)).toBe(2);
  const room=`call_${id}`;
  const identities=await Promise.all(pages.map(page=>page.evaluate(callId=>{const api=window.axonCallsProbe!;const ref=api.channel.endpoint!;return `${ref.id}:${ref.epoch}:${api.calls.get(callId)!.generation}`;},id)));
  const tokens=identities.map(identity=>token(identity,room));
  return {...peers,pages,id,room,identities,tokens};
 }catch(error){await peers.close();throw error;}
}
export async function connectMedia(peers:Awaited<ReturnType<typeof admissionPeers>>){
 await Promise.all(peers.pages.map((page,i)=>page.evaluate(raw=>window.axonLiveKitProbe!.join(raw),peers.tokens[i])));
 for(const page of peers.pages){
  await expect.poll(()=>page.evaluate(()=>window.axonLiveKitProbe!.room.remoteParticipants.size)).toBe(1);
  await page.evaluate(async callId=>{const api=window.axonCallsProbe!;await api.rpc.reportConnection({endpoint:api.channel.endpoint,requestId:crypto.randomUUID(),callId,generation:api.calls.get(callId)!.generation,ready:true});},peers.id);
 }
 for(const page of peers.pages){await expect.poll(()=>callState(page,peers.id)).toBe(3);await page.evaluate(()=>window.axonLiveKitProbe!.gate(true));}
}
export async function endMedia(peers:Awaited<ReturnType<typeof admissionPeers>>){
 await peers.a.page.evaluate(async callId=>{const api=window.axonCallsProbe!;await api.rpc.endCall({endpoint:api.channel.endpoint,requestId:crypto.randomUUID(),callId});},peers.id);
 await Promise.all(peers.pages.map(page=>page.evaluate(()=>window.axonLiveKitProbe!.end())));
 for(const page of peers.pages){
  await expect.poll(()=>callState(page,peers.id)).toBe(4);
  await page.evaluate(async callId=>{const api=window.axonCallsProbe!;await api.rpc.acknowledgeCleanup({endpoint:api.channel.endpoint,requestId:crypto.randomUUID(),callId,generation:api.calls.get(callId)!.generation});},peers.id);
 }
 await expect.poll(()=>callSQL(`SELECT count(*) FROM chat.call_user_slots WHERE call_id='${peers.id}'`)).toBe("0");
 const cleanup=await Promise.all(peers.pages.map(page=>page.evaluate(()=>({state:window.axonLiveKitProbe!.room.state,tracks:window.axonLiveKitProbe!.tracks.map(t=>({kind:t.kind,readyState:t.readyState})),elements:document.querySelectorAll("[data-livekit-probe]").length}))));
 for(const item of cleanup){expect(item.state).toBe("disconnected");expect(item.tracks.length).toBeGreaterThanOrEqual(4);expect(item.tracks.every(t=>t.readyState==="ended")).toBe(true);expect(item.elements).toBe(0);}
 return cleanup;
}
export function wrongIdentity(){return `${randomUUID()}:1:1`;}
