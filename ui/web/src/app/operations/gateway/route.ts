import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { readRpcMetrics, type GatewaySnapshot } from "@/lib/operations/telemetry";

const noCache = { "Cache-Control": "private, no-store" };

function status(code: number): Response {
  return new Response(null, { status: code, headers: noCache });
}

export async function GET(request: Request): Promise<Response> {
  const authorization = request.headers.get("authorization");
  if (!authorization?.startsWith("Bearer ")) return status(401);
  const rpcURL = process.env.AXON_GATEWAY_RPC_URL;
  if (!rpcURL) return status(503);

  try {
    const auth = createClient(AuthService, createConnectTransport({ baseUrl: rpcURL }));
    const { user } = await auth.getMe({}, { headers: { authorization }, timeoutMs: 2500, signal: request.signal });
    if (!user?.roles.includes("admin")) return status(403);
  } catch (error) {
    const code = ConnectError.from(error).code;
    if (code === Code.Unauthenticated) return status(401);
    if (code === Code.PermissionDenied) return status(403);
    return status(503);
  }

  const environment = process.env.AXON_GATEWAY_ENVIRONMENT || "Local";
  const metricsURL = process.env.AXON_GATEWAY_METRICS_URL;
  if (!metricsURL) {
    return Response.json({ state: "unconfigured", environment, capturedAt: new Date().toISOString() } satisfies GatewaySnapshot, { headers: noCache });
  }

  try {
    const upstream = await fetch(new URL("/metrics", metricsURL), {
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.any([request.signal, AbortSignal.timeout(2500)]),
      headers: { Accept: "text/plain" },
    });
    if (!upstream.ok || !upstream.headers.get("content-type")?.startsWith("text/plain")) return status(502);
    const routes = readRpcMetrics(await upstream.text());
    return Response.json({ state: "available", environment, routes, capturedAt: new Date().toISOString() } satisfies GatewaySnapshot, { headers: noCache });
  } catch {
    return status(502);
  }
}
