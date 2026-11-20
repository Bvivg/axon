"use client";

import { LogOut, Mic, MicOff, MonitorUp, PhoneOff, Settings2, Users, Video, VideoOff } from "lucide-react";
import { useState } from "react";

import { useCallAudio } from "@/components/calls/call-audio-renderer";
import { useCall } from "@/components/calls/call-provider";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { CallMode, CallState } from "@/gen/axon/calls/v1/calls_pb";
import { profileName } from "@/lib/format";
import { useRooms } from "@/lib/query/chat";
import { usePublicProfiles } from "@/lib/query/people";
import { callIssue } from "@/lib/calls/call-errors";

const control = "min-h-11 min-w-11 flex-col gap-0.5 px-2 py-1 text-[11px]";

export function CallEndButton({ compact = false }: { compact?: boolean }) {
  const { manager, view, incoming, elsewhere } = useCall();
  const call = view.call;
  if (!call || call.state === CallState.ENDED || elsewhere) return null;
  const leave = call.kind === "group" && call.ownerId !== manager.userID && !incoming;
  const label = incoming ? "Decline" : leave ? "Leave call" : call.state === CallState.RINGING ? "Cancel call" : call.kind === "group" ? "End for everyone" : "End call";
  const text = incoming ? "Decline" : leave ? "Leave" : call.state === CallState.RINGING ? "Cancel" : compact ? "End" : "End call";
  const end = <Button variant="destructive" size="sm" className="min-h-11 min-w-11" aria-label={label} disabled={view.ending} onClick={() => { void (leave ? manager.leave() : manager.end()); }}><PhoneOff />{text}</Button>;
  if (compact || leave || incoming || call.kind !== "group" || call.state === CallState.RINGING) return end;
  return <span className="inline-flex gap-2">
    <Button variant="outline" size="sm" className="min-h-11 min-w-11" aria-label="Leave call" disabled={view.ending} onClick={() => { void manager.leave(); }}><LogOut />Leave</Button>
    {end}
  </span>;
}

export function CallControls({ profiles, active, migrating }: { profiles?: Map<string, PublicProfile>; active: boolean; migrating: boolean }) {
  const { manager, view, keepScreenOn, setKeepScreenOn, screenHeld, shortcuts } = useCall();
  const audio = useCallAudio();
  const media = view.media;
  const video = view.call?.mode === CallMode.VIDEO;
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([]);
  const [outputSupported, setOutputSupported] = useState(false);
  const [people, setPeople] = useState(false);
  const [error, setError] = useState("");
  const [applying, setApplying] = useState(false);
  const canShare = video && !migrating && typeof navigator !== "undefined" && !!navigator.mediaDevices?.getDisplayMedia;
  const run = (operation: () => Promise<unknown>) => {
    if (applying) return;
    setApplying(true);
    setError("");
    void operation().catch(reason => setError(callIssue(reason, "capture").message)).finally(() => setApplying(false));
  };
  const inputs = (kind: "audio" | "video") => devices.filter(device => device.kind === (kind === "audio" ? "audioinput" : "videoinput") && device.deviceId);
  const inputID = (kind: "audio" | "video") => media?.local.getTracks().find(track => track.kind === kind)?.getSettings().deviceId ?? "";

  return (
    <footer className="call-controls" aria-label="Call controls">
      {view.resume ? <Button variant="signal" className="min-h-11" disabled={view.pending} onClick={() => { void manager.resume(); }}>Resume call</Button> : null}
      {media ? <>
        <Button variant="outline" className={control} data-call-mic aria-keyshortcuts={shortcuts ? "M" : undefined} aria-label={media.muted ? "Unmute microphone" : "Mute microphone"} aria-pressed={media.muted} onClick={() => media.mute()}>
          {media.muted ? <MicOff /> : <Mic />}<span>Microphone{media.muted ? " · Off" : ""}</span>
        </Button>
        {video ? <Button variant="outline" className={control} data-call-camera aria-keyshortcuts={shortcuts ? "V" : undefined} aria-label={media.cameraOff ? "Turn camera on" : "Turn camera off"} aria-pressed={media.cameraOff} disabled={migrating || applying} onClick={() => run(() => media.camera())}>
          {media.cameraOff ? <VideoOff /> : <Video />}<span>Camera{media.cameraOff ? " · Off" : ""}</span>
        </Button> : null}
        {video ? <Button variant="outline" className={`${control} call-share-button`} aria-label={media.sharing ? "Stop sharing screen" : "Share screen"} aria-pressed={media.sharing} disabled={!canShare || applying} onClick={() => run(() => media.share())}><MonitorUp /><span>{media.sharing ? "Stop share" : "Share"}</span></Button> : null}
        <DropdownMenu onOpenChange={open => {
          if (!open) return;
          setOutputSupported(typeof HTMLMediaElement.prototype.setSinkId === "function");
          void navigator.mediaDevices.enumerateDevices().then(setDevices).catch(reason => setError(callIssue(reason, "capture").message));
        }}>
          <DropdownMenuTrigger render={<Button variant="outline" className={control} aria-label="Call devices" disabled={migrating || applying}><Settings2 /><span>Devices</span></Button>} />
          <DropdownMenuContent side="top" align="center" className="max-h-[65dvh] overflow-y-auto">
            <DropdownMenuLabel>Microphone</DropdownMenuLabel>
            <DropdownMenuRadioGroup value={inputID("audio")} onValueChange={id => run(() => media.device("audio", id))}>
              {inputs("audio").map((device, index) => <DropdownMenuRadioItem key={device.deviceId} className="min-h-11 whitespace-normal" value={device.deviceId} disabled={migrating || applying}>{device.label || `Microphone ${index + 1}`}</DropdownMenuRadioItem>)}
            </DropdownMenuRadioGroup>
            {video ? <><DropdownMenuLabel>Camera</DropdownMenuLabel>
              <DropdownMenuRadioGroup value={inputID("video")} onValueChange={id => run(() => media.device("video", id))}>
                {inputs("video").map((device, index) => <DropdownMenuRadioItem key={device.deviceId} className="min-h-11 whitespace-normal" value={device.deviceId} disabled={migrating || applying}>{device.label || `Camera ${index + 1}`}</DropdownMenuRadioItem>)}
              </DropdownMenuRadioGroup>
              {!inputs("video").length ? <DropdownMenuItem disabled>Camera becomes available after access is allowed</DropdownMenuItem> : null}
            </> : null}
            <DropdownMenuLabel>Audio output</DropdownMenuLabel>
            {outputSupported ? <DropdownMenuRadioGroup value={audio.outputID} onValueChange={id => run(() => audio.setOutput(id))}>
              <DropdownMenuRadioItem className="min-h-11" value="">System default</DropdownMenuRadioItem>
              {devices.filter(d => d.kind === "audiooutput" && d.deviceId && d.deviceId !== "default").map((d, i) => <DropdownMenuRadioItem className="min-h-11 whitespace-normal" key={d.deviceId} value={d.deviceId}>{d.label || `Speaker ${i + 1}`}</DropdownMenuRadioItem>)}
            </DropdownMenuRadioGroup> : <DropdownMenuItem disabled>Uses your system audio output</DropdownMenuItem>}
            {video ? <DropdownMenuItem className="min-h-11" disabled={!canShare || applying} onClick={() => run(() => media.share())}>{media.sharing ? "Stop sharing screen" : "Share screen"}</DropdownMenuItem> : null}
            <DropdownMenuLabel>Screen</DropdownMenuLabel>
            {typeof navigator !== "undefined" && "wakeLock" in navigator ? <DropdownMenuRadioGroup value={keepScreenOn ? "on" : "off"} onValueChange={value => setKeepScreenOn(value === "on")}>
              <DropdownMenuRadioItem className="min-h-11" value="on">Keep screen on</DropdownMenuRadioItem>
              <DropdownMenuRadioItem className="min-h-11" value="off">Use system screen timeout</DropdownMenuRadioItem>
            </DropdownMenuRadioGroup> : <DropdownMenuItem disabled>Uses your system screen timeout</DropdownMenuItem>}
            {keepScreenOn && !screenHeld ? <DropdownMenuLabel role="status">Screen lock unavailable right now</DropdownMenuLabel> : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </> : null}
      <Dialog open={people} onOpenChange={setPeople}>
        <DialogTrigger render={<Button variant="outline" className={control}><Users /><span>People</span></Button>} />
        {people ? <CallPeople profiles={profiles} allowInvite={active && !migrating} close={() => setPeople(false)} /> : null}
      </Dialog>
      <div className="call-controls-end"><CallEndButton /></div>
      {applying ? <p role="status" className="basis-full text-xs text-muted-foreground">Updating media…</p> : null}
      {error ? <p role="alert" className="basis-full text-xs text-destructive">{error}</p> : null}
    </footer>
  );
}

function CallPeople({ profiles, allowInvite, close }: { profiles?: Map<string, PublicProfile>; allowInvite: boolean; close: () => void }) {
  const { manager, view } = useCall();
  const [invite, setInvite] = useState(false);
  const call = view.call;
  const canInvite = view.sfuAvailable && allowInvite && (call?.kind !== "group" || call.ownerId === manager.userID);
  return (
    <DialogContent className="max-h-[calc(100dvh-2rem)] space-y-3 overflow-y-auto">
      <DialogTitle>People</DialogTitle>
      <ul className="space-y-2 text-sm">
        {call?.participants.map(p => <li key={p.userId} className="flex items-center justify-between gap-2">
          <span className="min-w-0 [overflow-wrap:anywhere]">{p.userId === manager.userID ? "You" : profileName(profiles?.get(p.userId)) || "Participant"}{call.kind === "group" ? ` · ${p.status}` : ""}</span>
          {call.kind === "group" && call.ownerId === manager.userID && p.userId !== manager.userID && ["accepted", "invited"].includes(p.status) ? <Button size="sm" className="min-h-11" variant="ghost" disabled={view.pending} onClick={() => { void manager.removePerson(p.userId); }}>Remove</Button> : null}
        </li>)}
      </ul>
      {canInvite ? <Button variant="outline" className="min-h-11" disabled={view.pending} onClick={() => setInvite(!invite)}>Add person</Button> : null}
      {invite && canInvite ? <CallInvite onDone={close} /> : null}
      <DialogClose render={<Button variant="ghost" className="min-h-11" />}>Close</DialogClose>
    </DialogContent>
  );
}

function CallInvite({ onDone }: { onDone: () => void }) {
  const { manager, view } = useCall();
  const [inviteID, setInviteID] = useState("");
  const rooms = useRooms();
  const call = view.call;
  const contacts = (rooms.data ?? []).flatMap(room => room.peerUserId && !call?.participants.some(p => p.userId === room.peerUserId && ["accepted", "invited"].includes(p.status)) ? [room.peerUserId] : []);
  const profiles = usePublicProfiles(contacts).data;
  return (
    <div className="space-y-2 text-sm">
      {call?.kind !== "group" ? <p>Start a group call? This will end the current call.</p> : null}
      <select aria-label="Invite contact" value={inviteID} onChange={event => setInviteID(event.target.value)} className="min-h-11 w-full rounded-md border border-border bg-background p-2">
        <option value="">Select a contact</option>
        {contacts.map(id => <option key={id} value={id}>{profileName(profiles?.get(id)) || "Contact"}</option>)}
      </select>
      <Button className="min-h-11" disabled={!inviteID || view.pending} onClick={() => { void manager.addPerson(inviteID); onDone(); }}>{call?.kind === "group" ? "Invite" : "Start group call"}</Button>
    </div>
  );
}
