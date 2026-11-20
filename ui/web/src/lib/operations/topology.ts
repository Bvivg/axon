const gateway = "core/services/gateway/cmd/gateway/main.go";
const auth = "core/services/auth/cmd/auth/main.go";
const chat = "core/services/chat/cmd/chat/main.go";
const game = "core/services/game/cmd/game/main.go";

export const nodes = {
  web: { name: "Web", kind: "Client", source: "ui/web/src/lib/connect/clients.ts" },
  gateway: { name: "Gateway", kind: "Edge", source: gateway },
  auth: { name: "Auth", kind: "Service", source: auth },
  chat: { name: "Chat", kind: "Service", source: chat },
  game: { name: "Game", kind: "Service", source: game },
  postgres: { name: "Postgres", kind: "Infrastructure", source: "core/deploy/docker-compose.yml" },
  redis: { name: "Redis", kind: "Infrastructure", source: "core/deploy/docker-compose.yml" },
  kafka: { name: "Kafka", kind: "Infrastructure", source: "core/deploy/docker-compose.yml" },
  minio: { name: "MinIO", kind: "Infrastructure", source: "core/deploy/docker-compose.yml" },
} as const;

export type NodeId = keyof typeof nodes;
export type Edge = { from: NodeId; to: NodeId; transport: string; source: string; optional?: boolean };

export const edges: readonly Edge[] = [
  { from: "web", to: "gateway", transport: "Connect RPC, WebSocket", source: nodes.web.source },
  { from: "gateway", to: "auth", transport: "Connect RPC, JWKS", source: gateway },
  { from: "gateway", to: "chat", transport: "Chat and Calls RPC, WebSocket", source: gateway },
  { from: "gateway", to: "game", transport: "Connect RPC, WebSocket", source: gateway },
  { from: "auth", to: "postgres", transport: "SQL", source: auth },
  { from: "auth", to: "redis", transport: "Redis", source: auth },
  { from: "chat", to: "postgres", transport: "SQL", source: chat },
  { from: "chat", to: "redis", transport: "Redis Pub/Sub", source: chat },
  { from: "chat", to: "kafka", transport: "Kafka producer", source: chat, optional: true },
  { from: "chat", to: "minio", transport: "S3-compatible attachment storage", source: chat },
  { from: "game", to: "postgres", transport: "SQL", source: game },
];

export const layers = [["web"], ["gateway"], ["auth", "chat", "game"], ["postgres", "redis", "kafka", "minio"]] as const satisfies readonly (readonly NodeId[])[];
