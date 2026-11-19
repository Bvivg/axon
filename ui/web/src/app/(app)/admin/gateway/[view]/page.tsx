import { notFound } from "next/navigation";

import { GatewayDashboard } from "@/components/admin/gateway-dashboard";

export default async function GatewayView({ params }: { params: Promise<{ view: string }> }) {
  const { view } = await params;
  if (view !== "rpc" && view !== "security" && view !== "alerts") notFound();
  return <GatewayDashboard section={view} />;
}
