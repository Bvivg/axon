import { RequireAdmin } from "@/components/admin/admin-ui";
import { GatewayDashboard } from "@/components/admin/gateway-dashboard";

export default function GatewayPage() {
  return <RequireAdmin><GatewayDashboard section="overview" /></RequireAdmin>;
}
