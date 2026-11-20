import { Code, ConnectError } from "@connectrpc/connect";
import { useQuery } from "@tanstack/react-query";

import { AnalysisStatus, type GetAnalysisResponse } from "@/gen/axon/analysis/v1/analysis_pb";
import { analysisClient } from "@/lib/connect/clients";

export const analysisKey = (id: string) => ["analysis", id] as const;

export const analysisPollMs = 3_000;

export function awaitingAnalysis(report: GetAnalysisResponse | null | undefined) {
  return report === null || report?.status === AnalysisStatus.PENDING;
}

export function useAnalysis(id: string, enabled: boolean) {
  return useQuery({
    queryKey: analysisKey(id),
    queryFn: async ({ signal }): Promise<GetAnalysisResponse | null> => {
      try {
        return await analysisClient.getAnalysis({ sessionId: id }, { signal });
      } catch (error) {
        if (ConnectError.from(error).code === Code.NotFound) return null;
        throw error;
      }
    },
    enabled,
    refetchInterval: query => awaitingAnalysis(query.state.data) ? analysisPollMs : false,
    staleTime: 60_000,
  });
}
