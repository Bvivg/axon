import { notFound } from "next/navigation";

import { RequireAdmin } from "@/components/admin/admin-ui";
import { GatewayDashboard } from "@/components/admin/gateway-dashboard";

export default async function GatewayView({ params }: { params: Promise<{ view: string }> }) {
  const { view } = await params;
  if (view !== "rpc" && view !== "security" && view !== "alerts") notFound();
  return <RequireAdmin><GatewayDashboard section={view} /></RequireAdmin>;
}
