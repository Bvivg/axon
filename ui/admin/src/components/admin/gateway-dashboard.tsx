"use client";

import { useQuery } from "@tanstack/react-query";
import { Activity, Bell, RefreshCw, Route, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { Pane as WorkspacePage } from "@/components/ui/pane";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { useSession } from "@/lib/auth/session";
import { withBasePath } from "@/lib/base-path";
import { refreshSession } from "@/lib/auth/refresh";
import { getAccessToken } from "@/lib/auth/tokens";
import { rpcRoutes } from "@/lib/operations/contracts";
import type { GatewaySnapshot, RpcMetrics } from "@/lib/operations/telemetry";
import { clockTime } from "@/lib/format";
import { cn } from "@/lib/utils";

const sections = [
  { key: "overview", href: "/gateway", label: "Overview", icon: Activity },
  { key: "rpc", href: "/gateway/rpc", label: "RPC routes", icon: Route },
  { key: "security", href: "/gateway/security", label: "Auth & security", icon: ShieldCheck },
  { key: "alerts", href: "/gateway/alerts", label: "Alerts", icon: Bell },
] as const;

export type GatewaySection = typeof sections[number]["key"];

async function loadSnapshot(signal: AbortSignal): Promise<GatewaySnapshot> {
  const get = () => {
    const token = getAccessToken();
    return fetch(withBasePath("/operations/gateway"), { signal, cache: "no-store", headers: token ? { Authorization: `Bearer ${token}` } : {} });
  };
  let response = await get();
  if (response.status === 401 && await refreshSession()) response = await get();
  if (response.status === 403) throw new Error("You do not have access to Gateway telemetry.");
  if (response.status === 401) throw new Error("Sign in again to read Gateway telemetry.");
  if (response.status === 503) throw new Error("The Gateway authorization service is unavailable.");
  if (!response.ok) throw new Error("Could not load metrics from Gateway.");
  return response.json();
}

const format = new Intl.NumberFormat();
const show = (value?: number) => value === undefined ? "No samples" : format.format(value);
const routeSorts = [
  { value: "method", label: "Sort by route" },
  { value: "requests", label: "Most requests" },
];

function sum(values: (number | undefined)[]) {
  const known = values.filter((value) => value !== undefined);
  return known.length ? known.reduce((result, value) => result + value, 0) : undefined;
}

export function GatewayDashboard({ section }: { section: GatewaySection }) {
  const { user } = useSession();
  const snapshot = useQuery({
    queryKey: ["operations", "gateway", user?.id],
    queryFn: ({ signal }) => loadSnapshot(signal),
    staleTime: 20_000,
    refetchOnWindowFocus: false,
    retry: false,
  });
  const available = !snapshot.isError && snapshot.data?.state === "available" ? snapshot.data : undefined;
  const captured = available ? new Date(available.capturedAt) : undefined;

  return (
    <WorkspacePage title="Gateway" back="/" backLabel="Back to administration" width="wide" actions={
      <Button variant="ghost" size="icon" aria-label="Refresh" disabled={snapshot.isFetching} onClick={() => void snapshot.refetch()}>
        <RefreshCw aria-hidden className={cn(snapshot.isFetching && "motion-safe:animate-spin")} />
      </Button>
    }>
      <nav aria-label="Gateway" className="grid grid-cols-2 gap-1 border-b border-border pb-2 sm:flex sm:flex-wrap">
        {sections.map((item) => (
          <Link key={item.key} href={item.href} aria-current={section === item.key ? "page" : undefined}
            className={cn("flex min-h-11 items-center gap-2 rounded-lg px-3 text-sm font-medium hover:bg-accent", section === item.key ? "bg-accent text-signal" : "text-muted-foreground")}>
            <item.icon aria-hidden className="size-4 shrink-0" /> {item.label}
          </Link>
        ))}
      </nav>
      <dl className="flex min-h-5 flex-wrap items-center gap-x-6 gap-y-1 text-xs">
        <div className="flex gap-1.5"><dt className="text-muted-foreground">Environment</dt><dd className="font-medium">{snapshot.data?.environment ?? "Current environment"}</dd></div>
        <div className="flex gap-1.5"><dt className="text-muted-foreground">Range</dt><dd className="font-medium">Current snapshot</dd></div>
        {captured ? <div className="flex gap-1.5"><dt className="text-muted-foreground">Read from /metrics</dt><dd className="font-medium"><time dateTime={captured.toISOString()}>{clockTime(captured)}</time></dd></div> : null}
      </dl>
      {snapshot.isPending ? <p role="status" className="text-sm text-muted-foreground">Checking telemetry…</p> : null}
      {snapshot.isError ? <Alert>{snapshot.error.message}</Alert> : null}
      {snapshot.data?.state === "unconfigured" && !snapshot.isError ? <Alert variant="warning">Gateway metrics are unavailable in this environment. The route catalog is available from the API contracts.</Alert> : null}
      {section === "overview" ? <Overview routes={available?.routes} /> : null}
      {section === "rpc" ? <RouteTable routes={available?.routes} /> : null}
      {section === "security" ? <Security routes={available?.routes} /> : null}
      {section === "alerts" ? (
        <Card><EmptyState icon={Bell} title="Alert source unavailable"><p className="text-sm text-muted-foreground">Connect an alert source to view firing and resolved alerts.</p></EmptyState></Card>
      ) : null}
    </WorkspacePage>
  );
}

function Overview({ routes }: { routes?: Record<string, RpcMetrics> }) {
  const metrics = Object.values(routes ?? {});
  const counters = [
    { label: "RPC requests since restart", value: sum(metrics.map(({ total }) => total)) },
    { label: "RPCs in flight", value: sum(metrics.map(({ inFlight }) => inFlight)) },
    { label: "RPC routes with samples", value: routes ? metrics.length : undefined },
  ];
  return (
    <>
      <div className="grid gap-3 sm:grid-cols-3">
        {counters.map(({ label, value }) => (
          <Card key={label}>
            <CardHeader><CardTitle className="text-sm text-muted-foreground">{label}</CardTitle></CardHeader>
            <CardContent><p className={cn("tabular-nums", value === undefined ? "text-sm text-muted-foreground" : "text-3xl font-semibold")}>{routes ? show(value) : "Unavailable"}</p></CardContent>
          </Card>
        ))}
      </div>
      <p className="text-sm text-muted-foreground">Request rates, time-window percentiles, and SLOs require a historical metrics source.</p>
    </>
  );
}

function RouteTable({ routes }: { routes?: Record<string, RpcMetrics> }) {
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState("method");
  const visible = rpcRoutes.filter(({ path }) => path.toLowerCase().includes(query.trim().toLowerCase()));
  visible.sort((a, b) => sort === "requests"
    ? (routes?.[b.path]?.total ?? -1) - (routes?.[a.path]?.total ?? -1) || a.path.localeCompare(b.path)
    : a.path.localeCompare(b.path));

  return (
    <Card>
      <CardHeader><CardTitle>RPC route table</CardTitle></CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap gap-2">
          <Input type="search" aria-label="Filter RPC routes" placeholder="Filter RPC routes" className="w-full sm:max-w-xs" value={query} onChange={(event) => setQuery(event.target.value)} />
          <Select label="Sort routes" hideLabel value={sort} options={routeSorts} onChange={setSort} className="w-full sm:w-44" />
        </div>
        {!visible.length ? <EmptyState icon={Route} title="No routes match this filter" /> : (
          <ul aria-label="RPC procedures" className="divide-y divide-border">
            {visible.map(({ path, service, method }) => (
              <li className="grid gap-1 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(9rem,auto)] sm:gap-4" key={path}>
                <div className="min-w-0">
                  <p className="text-sm font-medium [overflow-wrap:anywhere]">{method} <span className="font-normal text-muted-foreground">· {service}</span></p>
                  <p className="font-mono text-xs text-muted-foreground [overflow-wrap:anywhere]">{path}</p>
                </div>
                <div className="text-sm tabular-nums">
                  {routes ? <>{show(routes[path]?.total)} {routes[path]?.total !== undefined ? "requests" : ""}</> : "Metrics unavailable"}
                  {routes?.[path]?.codes && Object.keys(routes[path].codes).length > 0 ? (
                    <ul className="flex flex-wrap gap-1 pt-1" aria-label={`Outcomes for ${method}`}>
                      {Object.entries(routes[path].codes).map(([code, count]) => <li key={code}><Badge variant="secondary">{code} · {format.format(count)}</Badge></li>)}
                    </ul>
                  ) : null}
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function Security({ routes }: { routes?: Record<string, RpcMetrics> }) {
  const authentication = rpcRoutes.filter(({ service, method }) => service === "AuthService" && ["Login", "RefreshToken", "StartOAuth", "CompleteOAuth"].includes(method));
  return (
    <Card>
      <CardHeader><CardTitle>Authentication outcomes</CardTitle></CardHeader>
      <CardContent className="space-y-3">
        <ul className="divide-y divide-border">
          {authentication.map(({ method, path }) => (
            <li key={path} className="flex flex-wrap items-center justify-between gap-2 py-2">
              <span className="text-sm font-medium">{method}</span>
              {routes?.[path]?.total !== undefined ? (
                <ul aria-label={`Outcomes for ${method}`} className="flex flex-wrap gap-1">
                  {Object.entries(routes[path].codes).map(([code, count]) => <li key={code}><Badge variant="secondary">{code} · {format.format(count)}</Badge></li>)}
                </ul>
              ) : <span className="text-sm text-muted-foreground">{routes ? "No samples" : "Metrics unavailable"}</span>}
            </li>
          ))}
        </ul>
        <p className="text-sm text-muted-foreground">CORS violations and rate-limit utilization have no configured data source.</p>
      </CardContent>
    </Card>
  );
}
