import fs from "node:fs";
import {execFileSync} from "node:child_process";
import {expect,test} from "@playwright/test";
import {admissionPeers,admin,connectMedia,endMedia,token,wrongIdentity} from "./calls-admission-support";

const paths=["/rtc","/rtc/validate","/rtc/v1","/rtc/v1/validate"];
test("native LiveKit join, all admission paths, bidirectional RTP and terminal fencing",async({browser})=>{
 const peers=await admissionPeers(browser);
 const trace:{path:string;status:number}[]=[];
 try{
  const before=await peers.a.page.request.get(`http://localhost:8080/livekit/rtc/validate?access_token=${encodeURIComponent(peers.tokens[0])}`);
  expect(before.status()).toBe(200);
  const legacy=await peers.a.page.evaluate(raw=>new Promise<number>((resolve,reject)=>{const socket=new WebSocket(`ws://localhost:8080/livekit/rtc?access_token=${encodeURIComponent(raw)}`);socket.binaryType="arraybuffer";socket.onmessage=event=>{const bytes=event.data instanceof ArrayBuffer?event.data.byteLength:String(event.data).length;socket.close();resolve(bytes);};socket.onerror=()=>reject(new Error("Legacy signaling failed"));}),peers.tokens[0]);
  expect(legacy).toBeGreaterThan(0);trace.push({path:"/livekit/rtc",status:101});
  const signaling:URL[]=[];
  for(const page of peers.pages)page.on("websocket",socket=>{const url=new URL(socket.url());if(url.pathname.startsWith("/livekit/")){trace.push({path:url.pathname,status:101});signaling.push(url);}});
  for(const page of peers.pages)page.on("response",response=>{const url=new URL(response.url());if(url.pathname.startsWith("/livekit/"))trace.push({path:url.pathname,status:response.status()});});
  await connectMedia(peers);
  const stats=()=>Promise.all(peers.pages.map(page=>page.evaluate(async()=>({...await window.axonLiveKitProbe!.stats() as object,playback:[...document.querySelectorAll<HTMLMediaElement>("[data-livekit-probe]")].map(element=>({kind:element.tagName,paused:element.paused,muted:element.muted,currentTime:element.currentTime}))}))));
  type Snapshot={reports:{rows:Record<string,unknown>[]}[];playback:{kind:string;paused:boolean;muted:boolean;currentTime:number}[]};
  const initial=await stats() as Snapshot[];
  await expect.poll(async()=>{
   const snapshots=await stats() as Snapshot[];
   return snapshots.every((snapshot,index)=>{
    const rows=snapshot.reports.flatMap(r=>r.rows),old=initial[index].reports.flatMap(r=>r.rows);
    const audio=rows.find(row=>row.type==="inbound-rtp" && row.kind==="audio"),video=rows.find(row=>row.type==="inbound-rtp" && row.kind==="video");
    const oldAudio=old.find(row=>row.type==="inbound-rtp" && row.kind==="audio"),oldVideo=old.find(row=>row.type==="inbound-rtp" && row.kind==="video");
    const transport=rows.find(row=>row.type==="transport" && row.selectedCandidatePairId);
    const pair=transport && rows.find(row=>row.id===transport.selectedCandidatePairId);
    const output=snapshot.playback.find(element=>element.kind==="AUDIO"),oldOutput=initial[index].playback.find(element=>element.kind==="AUDIO");
    return !!audio && !!video && Number(audio.bytesReceived)>Number(oldAudio?.bytesReceived??0) && Number(audio.totalAudioEnergy)>Number(oldAudio?.totalAudioEnergy??0) && Number(video.framesDecoded)>Number(oldVideo?.framesDecoded??0) && pair?.state==="succeeded" && !!output && !output.paused && !output.muted && output.currentTime>(oldOutput?.currentTime??0);
   });
  }).toBe(true);
  const media=await stats();
  const room=await admin(peers.a.page,"ListParticipants",{room:peers.room},{roomAdmin:true,room:peers.room});
  expect(room.participants).toHaveLength(2);
  const unsigned=await peers.a.page.request.post("http://chat:8082/internal/calls/livekit",{headers:{"content-type":"application/webhook+json"},data:{event:"participant_joined",room:{name:peers.room}}});expect(unsigned.status()).toBe(401);
  expect(signaling.length).toBeGreaterThanOrEqual(2);
  const allowed=[];
  for(const path of ["/rtc/validate","/rtc/v1/validate"]){const url=new URL(`http://localhost:8080/livekit${path}`);url.search=signaling[0].search;const status=await peers.a.page.evaluate(async address=>(await fetch(address)).status,url.toString());expect(status).toBe(200);allowed.push({path,status,browserFetch:true});}
  const cleanup=await endMedia(peers);
  const denied=[];
  for(const path of paths){const response=await peers.a.page.request.get(`http://localhost:8080/livekit${path}?access_token=${encodeURIComponent(peers.tokens[0])}`);expect(response.status()).toBe(403);denied.push({path,status:response.status()});}
  const browserDenied=await peers.a.page.evaluate(async raw=>(await fetch(`http://localhost:8080/livekit/rtc/validate?access_token=${encodeURIComponent(raw)}`)).status,peers.tokens[0]);expect(browserDenied).toBe(403);
  await expect.poll(async()=>{const rooms=await admin(peers.a.page,"ListRooms",{},{roomList:true});return (rooms.rooms??[]).some((room:{name:string})=>room.name===peers.room);}).toBe(false);
  fs.writeFileSync("/evidence/admission-native.json",JSON.stringify({id:peers.id,initial,media,room,cleanup,allowed,denied,trace,unsignedWebhookStatus:unsigned.status(),slots:0},null,2));
 }finally{await Promise.all(peers.pages.map(page=>page.evaluate(()=>window.axonLiveKitProbe?.end()).catch(()=>undefined)));await peers.close();}
});

test("a join admitted before End is removed when delayed signaling reaches LiveKit",async({browser})=>{
 const peers=await admissionPeers(browser);
 const rule=["OUTPUT","-p","tcp","-d","172.30.252.30","--dport","7880","-m","comment","--comment","axon-admission-late-join","-j","DROP"];
 let blocked=false;
 try{
  execFileSync("iptables",["-I",...rule]);blocked=true;
  await peers.a.page.evaluate(raw=>{
   const state={opened:false,messages:0,closed:false};
   (window as unknown as {axonLateJoin:typeof state}).axonLateJoin=state;
   const socket=new WebSocket(`ws://localhost:8080/livekit/rtc?access_token=${encodeURIComponent(raw)}`);
   socket.onopen=()=>{state.opened=true;};socket.onmessage=()=>{state.messages++;};socket.onclose=()=>{state.closed=true;};
  },peers.tokens[0]);
  await expect.poll(()=>{
   const output=execFileSync("iptables",["-L","OUTPUT","-n","-v","-x"],{encoding:"utf8"});
   const line=output.split("\n").find(line=>line.includes("axon-admission-late-join"));
   return line?Number(line.trim().split(/\s+/)[0]):0;
  }).toBeGreaterThan(0);
  const revokedAt=Date.now();
  await peers.a.page.evaluate(async callId=>{const api=window.axonCallsProbe!;await api.rpc.endCall({endpoint:api.channel.endpoint,requestId:crypto.randomUUID(),callId});},peers.id);
  execFileSync("iptables",["-D",...rule]);blocked=false;
  await expect.poll(()=>peers.a.page.evaluate(()=>(window as unknown as {axonLateJoin:{messages:number}}).axonLateJoin.messages)).toBeGreaterThan(0);
  await expect.poll(()=>peers.a.page.evaluate(()=>(window as unknown as {axonLateJoin:{closed:boolean}}).axonLateJoin.closed)).toBe(true);
  const removalObservedMs=Date.now()-revokedAt;
  const participants=await admin(peers.a.page,"ListParticipants",{room:peers.room},{roomAdmin:true,room:peers.room});
  expect(participants.participants??[]).toHaveLength(0);
  const status=await peers.a.page.evaluate(()=>(window as unknown as {axonLateJoin:object}).axonLateJoin);
  fs.writeFileSync("/evidence/admission-late-join.json",JSON.stringify({callID:peers.id,rule:"real TCP drop to private LiveKit after admission",status,removalObservedMs,participants},null,2));
 }finally{if(blocked)execFileSync("iptables",["-D",...rule]);await peers.close();}
});

test("forged and stale media credentials are refused on every signaling route",async({browser})=>{
 const peers=await admissionPeers(browser);
 try{
  const cases=[
   {name:"missing",raw:""},
   {name:"signature",raw:token(peers.identities[0],peers.room,{},"wrong")},
   {name:"expired",raw:token(peers.identities[0],peers.room,{exp:1})},
   {name:"issuer",raw:token(peers.identities[0],peers.room,{iss:"other"})},
   {name:"identity",raw:token(wrongIdentity(),peers.room)},
   {name:"epoch",raw:token(peers.identities[0].replace(/:\d+:/,":999:"),peers.room)},
   {name:"generation",raw:token(peers.identities[0].replace(/:\d+$/,":999"),peers.room)},
   {name:"room",raw:token(peers.identities[0],"call_00000000-0000-4000-8000-000000000000")},
   {name:"admin",raw:token(peers.identities[0],peers.room,{video:{roomJoin:true,room:peers.room,roomAdmin:true}})},
  ];
  const matrix=[];
  for(const item of cases)for(const path of paths){const response=await peers.a.page.request.get(`http://localhost:8080/livekit${path}?access_token=${encodeURIComponent(item.raw)}`);expect(response.status()).toBe(item.raw?403:401);matrix.push({case:item.name,path,status:response.status()});}
  const bypass=await peers.a.page.request.post("http://localhost:8080/livekit/twirp/livekit.RoomService/ListRooms",{data:{}});expect(bypass.status()).toBe(404);
  const internal=await peers.a.page.request.post("http://localhost:8080/axon.calls.v1.MediaAdmissionService/CheckMediaAdmission",{data:{token:peers.tokens[0]}});expect(internal.status()).toBe(404);
  await expect(peers.a.page.request.get("http://localhost:7880/rtc/validate")).rejects.toThrow("ECONNREFUSED");
  fs.writeFileSync("/evidence/admission-deny.json",JSON.stringify({matrix,serverApi:bypass.status(),privateRpc:internal.status(),directPort:"ECONNREFUSED"},null,2));
 }finally{await peers.a.page.evaluate(async callId=>{const api=window.axonCallsProbe!;await api.rpc.endCall({endpoint:api.channel.endpoint,requestId:crypto.randomUUID(),callId});},peers.id).catch(()=>undefined);await peers.close();}
});
