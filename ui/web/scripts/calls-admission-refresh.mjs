import fs from "node:fs";
import {createHmac,randomUUID} from "node:crypto";
import {chromium} from "playwright";

const origin="http://localhost:3000",gateway="http://localhost:8080";
const credentials=JSON.parse(fs.readFileSync("/evidence/.secrets/admission.json","utf8"));
const started=Date.now(),deadline=started+12*60*1000;
const evidence={startedAt:new Date(started).toISOString(),limitMs:12*60*1000,refresh:[],reconnect:[],denied:[]};
const save=()=>fs.writeFileSync("/evidence/admission-refresh.json",JSON.stringify(evidence,null,2));
const watchdog=setTimeout(()=>{evidence.passed=false;evidence.error="Declared12min real-clock budget exceeded";save();process.exit(1);},12*60*1000);
function token(identity,room,video){const now=Math.floor(Date.now()/1000);const head=Buffer.from(JSON.stringify({alg:"HS256",typ:"JWT"})).toString("base64url");const claims=Buffer.from(JSON.stringify({iss:credentials.key,sub:identity,nbf:now-1,exp:now+120,video:video??{roomJoin:true,room,canPublish:true,canSubscribe:true,canPublishData:false}})).toString("base64url");const body=`${head}.${claims}`;return `${body}.${createHmac("sha256",credentials.secret).update(body).digest("base64url")}`;}
async function until(fn,timeout=10000){const end=Math.min(deadline,Date.now()+timeout);while(Date.now()<end){if(await fn())return;await new Promise(resolve=>setTimeout(resolve,100));}throw new Error("Condition deadline exceeded");}
async function admin(method,data,grant){const response=await fetch(`http://livekit:7880/twirp/livekit.RoomService/${method}`,{method:"POST",headers:{"content-type":"application/json",Authorization:`Bearer ${token("refresh-probe","",grant)}`},body:JSON.stringify(data)});if(!response.ok)throw new Error(`Room API ${response.status}`);return response.json();}
const browser=await chromium.launch({channel:"chromium",args:["--use-fake-device-for-media-stream","--use-file-for-fake-audio-capture=/evidence/.secrets/tone.wav"]});
const people=[];
try{
 console.log("Real-clock refresh probe started; limit12min, no E2E timeout changes");
 for(let i=0;i<2;i++){
  const context=await browser.newContext({permissions:["microphone","camera"]});
  const suffix=randomUUID().replaceAll("-","").slice(0,12);
  const email=`admission-${suffix}@axon.test`,username=`adm_${suffix}`,name=`Refresh ${suffix}`;
  const response=await context.request.post(`${gateway}/axon.auth.v1.AuthService/Register`,{headers:{Origin:origin,"Connect-Protocol-Version":"1"},data:{email,username,password:"correct-horse-battery-staple",displayName:name}});
  if(!response.ok())throw new Error(`Register ${response.status()}`);
  const registered=await response.json();
  const page=await context.newPage();page.setDefaultTimeout(30000);
  const person={context,page,email,username,name,userID:registered.user.id,sockets:[]};
  page.on("websocket",socket=>{const url=new URL(socket.url());if(url.pathname.startsWith("/livekit/"))person.sockets.push({at:Date.now(),path:url.pathname,reconnect:url.searchParams.get("reconnect"),claims:JSON.parse(Buffer.from(url.searchParams.get("access_token").split(".")[1],"base64url").toString())});});
  people.push(person);
 }
 const [a,b]=people;
 await a.page.goto(`${origin}/chat`);
 await a.page.getByRole("button",{name:"New chat"}).click();
 const composer=a.page.getByRole("complementary",{name:"Chats"}).getByRole("region",{name:"New conversation"});
 await composer.getByLabel("Search people").fill(b.email);
 await composer.getByRole("button",{name:b.name}).click();
 await a.page.getByLabel("Message",{exact:true}).fill("Admission refresh fixture");
 await a.page.getByRole("button",{name:"Send",exact:true}).click();
 const roomLink=a.page.getByRole("complementary",{name:"Chats"}).locator(`a[href="/chat#${b.username}"]`);
 await until(async()=>!!await roomLink.getAttribute("data-room-id"));
 const chatRoom=await roomLink.getAttribute("data-room-id");
 for(const person of people){await person.page.goto(`${origin}/calls/spike`);await until(()=>person.page.evaluate(()=>!!window.axonLiveKitProbe && !!window.axonCallsProbe?.channel.endpoint));}
 const callID=await a.page.evaluate(async roomId=>{const api=window.axonCallsProbe;return (await api.rpc.startCall({endpoint:api.channel.endpoint,requestId:crypto.randomUUID(),roomId,mode:2})).call.id;},chatRoom);
 evidence.callID=callID;evidence.room=`call_${callID}`;
 await until(()=>b.page.evaluate(id=>window.axonCallsProbe.calls.get(id)?.state===1,callID));
 await b.page.evaluate(async id=>{const api=window.axonCallsProbe;await api.rpc.acceptCall({endpoint:api.channel.endpoint,requestId:crypto.randomUUID(),callId:id});},callID);
 for(const person of people){await until(()=>person.page.evaluate(id=>window.axonCallsProbe.calls.get(id)?.state===2,callID));person.identity=await person.page.evaluate(id=>{const api=window.axonCallsProbe,ref=api.channel.endpoint;return `${ref.id}:${ref.epoch}:${api.calls.get(id).generation}`;},callID);person.token=token(person.identity,evidence.room);}
 await Promise.all(people.map(person=>person.page.evaluate(raw=>window.axonLiveKitProbe.join(raw),person.token)));
 for(const person of people){await person.page.evaluate(async id=>{const api=window.axonCallsProbe;await api.rpc.reportConnection({endpoint:api.channel.endpoint,requestId:crypto.randomUUID(),callId:id,generation:api.calls.get(id).generation,ready:true});},callID);}
 for(const person of people){await until(()=>person.page.evaluate(id=>window.axonCallsProbe.calls.get(id)?.state===3,callID));await person.page.evaluate(()=>window.axonLiveKitProbe.gate(true));}
 evidence.initial=await Promise.all(people.map(person=>person.page.evaluate(()=>window.axonLiveKitProbe.stats())));save();
 const observeUntil=Math.min(deadline-30000,Date.now()+11*60*1000);
 let checkpoint=Date.now();
 while(Date.now()<observeUntil){
  evidence.refresh=await a.page.evaluate(()=>window.axonLiveKitProbe.refresh);
  if(evidence.refresh.length)break;
  if(Date.now()-checkpoint>=60000){console.log(`Waiting for real server refresh: ${Math.floor((Date.now()-started)/1000)}s`);save();checkpoint=Date.now();}
  await new Promise(resolve=>setTimeout(resolve,1000));
 }
 if(!evidence.refresh.length)throw new Error("Server-generated token refresh not observed within declared12min budget");
 for(const claims of evidence.refresh){if(claims.identity!==a.identity || claims.room!==evidence.room)throw new Error("Refreshed token lost binding");}
 const refreshed=await a.page.evaluate(()=>window.axonLiveKitProbe.refreshedToken);
 if(!refreshed || refreshed===a.token)throw new Error("No distinct server-generated token");
 console.log("Real server refresh observed",JSON.stringify(evidence.refresh));save();
 for(const scenario of ["signal-reconnect","full-reconnect"]){
  const before=Date.now();
  const count=a.sockets.length;
  await a.page.evaluate(value=>window.axonLiveKitProbe.room.simulateScenario(value),scenario);
  await until(()=>a.sockets.length>count);
  await until(()=>a.page.evaluate(()=>window.axonLiveKitProbe.room.state==="connected" && window.axonLiveKitProbe.room.remoteParticipants.size===1));
  const baseline=await a.page.evaluate(()=>window.axonLiveKitProbe.stats());
  const audioBytes=snapshot=>snapshot.reports.flatMap(report=>report.rows).filter(row=>row.type==="inbound-rtp" && row.kind==="audio").reduce((sum,row)=>sum+Number(row.bytesReceived),0);
  await until(async()=>audioBytes(await a.page.evaluate(()=>window.axonLiveKitProbe.stats()))>audioBytes(baseline));
  const ws=a.sockets.at(-1);
  if(ws.claims.sub!==a.identity || ws.claims.video.room!==evidence.room || ws.claims.exp<evidence.refresh[0].exp)throw new Error("Reconnect did not use a bound refreshed token");
  evidence.reconnect.push({scenario,elapsedMs:Date.now()-before,websocket:{at:ws.at,path:ws.path,reconnect:ws.reconnect,exp:ws.claims.exp,identity:ws.claims.sub},baseline,stats:await a.page.evaluate(()=>window.axonLiveKitProbe.stats())});save();
 }
 for(const person of people)await person.page.evaluate(()=>{window.axonRetainedAdmission=window.axonLiveKitProbe;});
 const revokeAt=Date.now();
 const logout=await a.context.request.post(`${gateway}/axon.auth.v1.AuthService/Logout`,{headers:{Origin:origin,"Connect-Protocol-Version":"1"},data:{}});
 if(!logout.ok())throw new Error(`Logout ${logout.status()}`);
 await until(async()=>{const result=await admin("ListParticipants",{room:evidence.room},{roomAdmin:true,room:evidence.room});return !result.participants?.some(p=>p.identity===a.identity);});
 evidence.revokeRemovalMs=Date.now()-revokeAt;
 for(const [kind,raw] of [["original",a.token],["refreshed",refreshed]])for(const path of ["/rtc","/rtc/validate","/rtc/v1","/rtc/v1/validate"]){const response=await b.context.request.get(`${gateway}/livekit${path}?access_token=${encodeURIComponent(raw)}`);if(response.status()!==403)throw new Error(`Revoke bypass ${kind} ${path} ${response.status()}`);evidence.denied.push({kind,path,status:response.status()});}
 evidence.afterRevoke=await Promise.all(people.map(person=>person.page.evaluate(()=>window.axonRetainedAdmission.stats())));
 for(const person of people)await person.page.evaluate(()=>window.axonRetainedAdmission.end());
 evidence.cleanup=await Promise.all(people.map(person=>person.page.evaluate(()=>({state:window.axonRetainedAdmission.room.state,tracks:window.axonRetainedAdmission.tracks.map(t=>({kind:t.kind,readyState:t.readyState})),elements:document.querySelectorAll("[data-livekit-probe]").length}))));
 for(const cleanup of evidence.cleanup){if(cleanup.state!=="disconnected" || cleanup.tracks.length<4 || cleanup.tracks.some(track=>track.readyState!=="ended") || cleanup.elements!==0)throw new Error("Native cleanup not proven");}
 evidence.passed=true;evidence.elapsedMs=Date.now()-started;save();console.log("Refresh/reconnect/revoke probe PASS",evidence.elapsedMs,"ms; removal",evidence.revokeRemovalMs,"ms");
}catch(error){evidence.passed=false;evidence.error=error.message;save();console.error(error.message);process.exitCode=1;}
finally{clearTimeout(watchdog);for(const person of people){await person.page.evaluate(()=>window.axonLiveKitProbe?.end()).catch(()=>undefined);await person.context.close();}await browser.close();}
