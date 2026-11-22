import { RequireAdmin } from "@/components/admin/admin-ui";
import { ServiceMap } from "@/components/admin/service-map";

export default function ServiceMapPage() {
  return <RequireAdmin><ServiceMap /></RequireAdmin>;
}
