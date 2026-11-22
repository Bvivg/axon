import { Suspense } from "react";

import { RequirePermission } from "@/components/admin/admin-ui";
import { AnalysisReports } from "@/components/admin/play/analysis-reports";
import { Permission } from "@/lib/admin/permissions";

export default function AnalysisPage() {
  return (
    <RequirePermission permission={Permission.GamesRead}>
      <Suspense><AnalysisReports /></Suspense>
    </RequirePermission>
  );
}
