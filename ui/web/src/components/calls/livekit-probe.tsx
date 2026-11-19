"use client";

import { useEffect } from "react";
import { Room, RoomEvent, createLocalTracks, type LocalTrack } from "livekit-client";

type SignalClient = {onTokenRefresh?: (token:string)=>void};
type Engine = {client:SignalClient;pcManager?:{publisher?:{pc:RTCPeerConnection};subscriber?:{pc:RTCPeerConnection}}};
export interface LiveKitProbeAPI {
 room: Room;
 tracks: MediaStreamTrack[];
 local: LocalTrack[];
 refresh: {at:number;exp:number;nbf?:number;identity:string;room:string}[];
 refreshedToken?:string;
 join(token:string):Promise<void>;
 gate(enabled:boolean):void;
 stats():Promise<unknown>;
 end():Promise<void>;
}
declare global {interface Window {axonLiveKitProbe?:LiveKitProbeAPI;}}

export function LiveKitProbe() {
 useEffect(()=>{
  const room=new Room();
  const engine=(room as unknown as {engine:Engine}).engine;
  const api:LiveKitProbeAPI={
   room,tracks:[],local:[],refresh:[],
   async join(token){
    await room.connect("ws://localhost:8080/livekit",token,{autoSubscribe:true});
    const original=engine.client.onTokenRefresh;
    engine.client.onTokenRefresh=next=>{
     original?.call(engine.client,next);
     api.refreshedToken=next;
     const claims=JSON.parse(atob(next.split(".")[1].replace(/-/g,"+").replace(/_/g,"/")));
     api.refresh.push({at:Date.now(),exp:claims.exp,nbf:claims.nbf,identity:claims.sub,room:claims.video.room});
    };
    api.local=await createLocalTracks({audio:true,video:true});
    for(const track of api.local){api.tracks.push(track.mediaStreamTrack);track.mediaStreamTrack.enabled=false;await room.localParticipant.publishTrack(track);}
   },
   gate(enabled){for(const track of api.local)track.mediaStreamTrack.enabled=enabled;},
   async stats(){
    const current=(room as unknown as {engine?:Engine}).engine;
    const pcs=[...new Set([current?.pcManager?.publisher?.pc,current?.pcManager?.subscriber?.pc].filter((pc):pc is RTCPeerConnection=>!!pc))];
    const reports=[];
    for(const pc of pcs){
     const all=await pc.getStats();
     const rows:Record<string,unknown>[]=[];
     all.forEach(stat=>{if(["transport","candidate-pair","local-candidate","remote-candidate","inbound-rtp","outbound-rtp","media-source"].includes(stat.type))rows.push({...stat});});
     reports.push({connectionState:pc.connectionState,rows});
    }
    return {state:room.state,sid:await room.getSid(),identity:room.localParticipant.identity,participants:[...room.remoteParticipants.values()].map(p=>({identity:p.identity,sid:p.sid})),reports,tracks:api.tracks.map(t=>({kind:t.kind,readyState:t.readyState})),refresh:api.refresh};
   },
   async end(){await room.disconnect(true);for(const track of api.tracks)track.stop();for(const element of document.querySelectorAll("[data-livekit-probe]"))element.remove();},
  };
  room.on(RoomEvent.TrackSubscribed,track=>{api.tracks.push(track.mediaStreamTrack);const element=track.attach();element.dataset.livekitProbe="1";element.hidden=true;document.body.append(element);void element.play().catch(()=>undefined);});
  window.axonLiveKitProbe=api;
  return()=>{void api.end();delete window.axonLiveKitProbe;};
 },[]);
 return null;
}
