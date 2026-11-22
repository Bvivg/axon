"use client";

import { ArrowRight, Box, Database, Laptop, Network, Route } from "lucide-react";
import { useState } from "react";

import { Pane as WorkspacePage } from "@/components/ui/pane";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { publicServices } from "@/lib/operations/contracts";
import { edges, layers, nodes, type NodeId } from "@/lib/operations/topology";
import { cn } from "@/lib/utils";

const kindIcon = { Client: Laptop, Edge: Route, Service: Box, Infrastructure: Database };
const graphWidth = 840;
const graphHeight = layers.length * 88;
const coordinates = new Map<NodeId, [number, number]>(layers.flatMap((layer, row) => layer.map((id, index) => [id, [(index + 1) * graphWidth / (layer.length + 1), 44 + row * 88]] as [NodeId, [number, number]])));

type Selection = { node: NodeId } | { edge: number };
const catalog = Object.keys(nodes) as NodeId[];
const optionClass = "min-h-11 rounded-lg border border-input bg-card px-2 text-sm";

export function ServiceMap() {
  const [filter, setFilter] = useState("all");
  const [query, setQuery] = useState("");
  const [selection, setSelection] = useState<Selection>({ node: "gateway" });
  const choices = catalog.filter((id) => (
    (filter === "all" || nodes[id].kind === filter) &&
    nodes[id].name.toLowerCase().includes(query.trim().toLowerCase())
  ));
  const activeNode = "node" in selection ? selection.node : undefined;
  const activeEdge = "edge" in selection ? edges[selection.edge] : undefined;

  return (
    <WorkspacePage title="Service map" back="/profile" backLabel="Back to profile" width="wide">
      <Tabs defaultValue="code" className="space-y-4">
        <TabsList className="flex h-auto max-w-full flex-wrap">
          <TabsTrigger className="min-h-9" value="code">Code topology</TabsTrigger>
          <TabsTrigger className="min-h-9" value="contracts">Contracts</TabsTrigger>
          <TabsTrigger className="min-h-9" value="runtime">Runtime</TabsTrigger>
        </TabsList>
        <TabsContent value="code">
          <dl className="mb-4 grid grid-cols-3 gap-2">
            {[["Services", catalog.length], ["Connections", edges.length], ["Contracts", publicServices.length]].map(([label, count]) => (
              <div key={label} className="rounded-[14px] border border-line bg-surface p-3"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="mt-1 text-xl font-semibold">{count}</dd></div>
            ))}
          </dl>
          <div className="mb-4 flex flex-wrap gap-2">
            <Input type="search" aria-label="Find a service" placeholder="Find a service" className="min-h-11 w-full sm:max-w-xs" value={query} onChange={(event) => setQuery(event.target.value)} />
            <select className={optionClass} aria-label="Service type" value={filter} onChange={(event) => setFilter(event.target.value)}>
              <option value="all">All service types</option>
              <option value="Client">Client</option>
              <option value="Edge">Edge</option>
              <option value="Service">Service</option>
              <option value="Infrastructure">Infrastructure</option>
            </select>
          </div>
          <div className="grid items-start gap-4 min-[1151px]:grid-cols-[minmax(0,1fr)_minmax(238px,290px)]">
            <Card className="min-w-0">
              <CardHeader><CardTitle>Topology</CardTitle></CardHeader>
              <CardContent>
                <TopologyGraph activeNode={activeNode} activeEdge={activeEdge} shown={choices} onSelect={setSelection} />
                <h3 className="mt-4 text-sm font-semibold">Service catalog</h3>
                {choices.length ? (
                  <ul aria-label="Service catalog" className="mt-2 grid gap-2 sm:grid-cols-2">
                    {choices.map((id) => (
                      <li key={id}>
                        <button type="button" aria-pressed={id === activeNode}
                          className={cn("flex min-h-11 w-full items-center justify-between gap-2 rounded-lg border border-border px-3 text-left text-sm hover:bg-accent", id === activeNode && "border-signal bg-accent text-signal")}
                          onClick={() => setSelection({ node: id })}>
                          <span className="min-w-0 [overflow-wrap:anywhere]">{nodes[id].name}</span>
                          <span className="text-xs text-muted-foreground">{nodes[id].kind}</span>
                        </button>
                      </li>
                    ))}
                  </ul>
                ) : <p role="status" className="py-3 text-sm text-muted-foreground">No services match these filters.</p>}
              </CardContent>
            </Card>
            <Card className="min-w-0" aria-label="Topology details">
              <CardHeader>
                <CardTitle>{activeNode ? nodes[activeNode].name : "Connection details"}</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                {activeNode ? (
                  <>
                    <Badge variant="secondary">{nodes[activeNode].kind}</Badge>
                    <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]">{nodes[activeNode].source}</p>
                    <h3 className="text-sm font-semibold">Connections</h3>
                    <ul className="space-y-1.5">
                      {edges.map((edge, index) => edge.from === activeNode || edge.to === activeNode ? (
                        <li key={index}>
                          <Button variant="outline" className="h-auto min-h-11 w-full flex-wrap justify-start whitespace-normal text-left" onClick={() => setSelection({ edge: index })}>
                            {nodes[edge.from].name} <ArrowRight aria-label="to" /> {nodes[edge.to].name}
                            <span className="basis-full text-xs text-muted-foreground">{edge.transport}{edge.optional ? " · Optional" : ""}</span>
                          </Button>
                        </li>
                      ) : null)}
                    </ul>
                  </>
                ) : null}
                {activeEdge ? (
                  <>
                    <p className="font-semibold">{nodes[activeEdge.from].name} → {nodes[activeEdge.to].name}</p>
                    <dl className="grid gap-2 text-sm">
                      <div><dt className="text-muted-foreground">Protocol</dt><dd>{activeEdge.transport}</dd></div>
                      <div><dt className="text-muted-foreground">Evidence</dt><dd>{activeEdge.optional ? "Conditional in source" : "Confirmed in source"}</dd></div>
                      <div><dt className="text-muted-foreground">Source</dt><dd className="font-mono text-xs [overflow-wrap:anywhere]">{activeEdge.source}</dd></div>
                    </dl>
                    <div className="flex flex-wrap gap-2">
                      {[activeEdge.from, activeEdge.to].map((id) => <Button variant="outline" className="min-h-11" key={id} onClick={() => setSelection({ node: id })}>Open {nodes[id].name}</Button>)}
                    </div>
                  </>
                ) : null}
              </CardContent>
            </Card>
          </div>
        </TabsContent>
        <TabsContent value="contracts">
          <div className="grid gap-3 lg:grid-cols-2">
            {publicServices.map((service) => (
              <Card key={service.typeName} className="min-w-0">
                <CardHeader><CardTitle>{service.name}</CardTitle></CardHeader>
                <CardContent>
                  <p className="mb-3 font-mono text-xs text-muted-foreground [overflow-wrap:anywhere]">{service.typeName}</p>
                  <ul className="flex flex-wrap gap-1.5">
                    {service.methods.map((method) => <li key={method.name} className="min-w-0"><Badge className="h-auto min-h-6 whitespace-normal [overflow-wrap:anywhere]" variant="outline">{method.name}</Badge></li>)}
                  </ul>
                </CardContent>
              </Card>
            ))}
          </div>
        </TabsContent>
        <TabsContent value="runtime">
          <Card><EmptyState icon={Network} title="Runtime topology unavailable"><p className="text-sm text-muted-foreground">A trace source is required to show observed service connections.</p></EmptyState></Card>
        </TabsContent>
      </Tabs>
    </WorkspacePage>
  );
}

function TopologyGraph({ activeNode, activeEdge, shown, onSelect }: { activeNode?: NodeId; activeEdge?: typeof edges[number]; shown: NodeId[]; onSelect: (value: Selection) => void }) {
  return (
    <div className="service-graph" aria-label="Topology overview" style={{ minHeight: graphHeight }}>
      <svg width="100%" height="100%" viewBox={`0 0 ${graphWidth} ${graphHeight}`} preserveAspectRatio="none" aria-hidden>
        {edges.map((edge) => {
          const [x1, y1] = coordinates.get(edge.from)!;
          const [x2, y2] = coordinates.get(edge.to)!;
          const visible = shown.includes(edge.from) || shown.includes(edge.to);
          const active = activeEdge === edge || edge.from === activeNode || edge.to === activeNode;
          const mid = (y1 + y2) / 2;
          return <path key={`${edge.from}-${edge.to}`} d={`M ${x1} ${y1} C ${x1} ${mid}, ${x2} ${mid}, ${x2} ${y2}`} fill="none" stroke={active ? "var(--color-signal)" : "var(--color-input)"} strokeWidth={active ? 2 : 1} opacity={visible ? .8 : .22} strokeDasharray={edge.optional ? "5 4" : undefined} vectorEffect="non-scaling-stroke" />;
        })}
      </svg>
      {catalog.map((id) => {
        const [x, y] = coordinates.get(id)!;
        const Icon = kindIcon[nodes[id].kind];
        return <button type="button" key={id} className="service-graph-node" aria-pressed={activeNode === id} data-filtered={!shown.includes(id)} onClick={() => onSelect({ node: id })} style={{ left: `${x / graphWidth * 100}%`, top: `${y / graphHeight * 100}%` }}>
          <Icon aria-hidden size={15} />{nodes[id].name}
        </button>;
      })}
    </div>
  );
}
