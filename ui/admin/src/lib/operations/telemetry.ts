import { rpcRoutes } from "@/lib/operations/contracts";

export type RpcMetrics = { total?: number; inFlight?: number; codes: Record<string, number> };

export type GatewaySnapshot = {
  environment: string;
  capturedAt: string;
} & (
  | { state: "available"; routes: Record<string, RpcMetrics> }
  | { state: "unconfigured" }
);

const procedures = new Set(rpcRoutes.map(({ path }) => path));

export function readRpcMetrics(body: string): Record<string, RpcMetrics> {
  const routes: Record<string, RpcMetrics> = {};
  for (const line of body.split("\n")) {
    const sample = /^(rpc_requests_total|rpc_in_flight)\{([^}]*)\}\s+(\S+)\s*$/.exec(line);
    if (!sample) continue;

    const [, metric, labels, value] = sample;
    const procedure = /(?:^|,)procedure="([^"]+)"/.exec(labels)?.[1];
    if (!procedure || !procedures.has(procedure) || !/(?:^|,)service="gateway"(?:,|$)/.test(labels)) continue;
    const number = Number(value);
    if (!Number.isSafeInteger(number) || number < 0) continue;

    const code = /(?:^|,)code="([a-z_]+)"(?:,|$)/.exec(labels)?.[1];
    if (metric === "rpc_requests_total" && !code) continue;
    const data = routes[procedure] ??= { codes: {} };
    if (metric === "rpc_in_flight") {
      data.inFlight = number;
    } else if (code) {
      data.codes[code] = number;
    }
  }
  for (const data of Object.values(routes)) {
    const counts = Object.values(data.codes);
    if (counts.length) data.total = counts.reduce((total, count) => total + count, 0);
  }
  return routes;
}
