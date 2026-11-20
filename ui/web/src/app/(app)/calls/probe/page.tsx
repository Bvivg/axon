import { notFound } from "next/navigation";
import { CallsProbe } from "@/components/calls/calls-probe";

export const dynamic = "force-dynamic";

export default function CallsProbePage() {
  if (process.env.CALLS_E2E_PROBE !== "1") notFound();
  return <CallsProbe />;
}
