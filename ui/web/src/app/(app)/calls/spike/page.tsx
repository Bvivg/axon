import { notFound } from "next/navigation";
import { CallsProbe } from "@/components/calls/calls-probe";
import { LiveKitProbe } from "@/components/calls/livekit-probe";
export const dynamic="force-dynamic";
export default function CallsAdmissionPage(){
 if(process.env.CALLS_E2E_PROBE!=="1")notFound();
 return <><CallsProbe/><LiveKitProbe/></>;
}
