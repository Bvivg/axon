const gateway = "core/services/gateway/cmd/gateway/main.go";
const auth = "core/services/auth/cmd/auth/main.go";
const chat = "core/services/chat/cmd/chat/main.go";
const game = "core/services/game/cmd/game/main.go";
const rating = "core/services/rating/cmd/rating/main.go";
const analysis = "core/services/analysis/cmd/analysis/main.go";
const tournament = "core/services/tournament/cmd/tournament/main.go";
const compose = "core/deploy/docker-compose.yml";
const connector = "core/deploy/cdc/outbox-connector.json";

export const nodes = {
  web: { name: "Web", kind: "Client", source: "ui/web/src/lib/connect/clients.ts" },
  gateway: { name: "Gateway", kind: "Edge", source: gateway },
  auth: { name: "Auth", kind: "Service", source: auth },
  chat: { name: "Chat", kind: "Service", source: chat },
  game: { name: "Game", kind: "Service", source: game },
  tournament: { name: "Tournament", kind: "Service", source: tournament },
  analysis: { name: "Analysis", kind: "Service", source: analysis },
  rating: { name: "Rating", kind: "Service", source: rating },
  postgres: { name: "Postgres", kind: "Infrastructure", source: compose },
  redis: { name: "Redis", kind: "Infrastructure", source: compose },
  kafka: { name: "Kafka", kind: "Infrastructure", source: compose },
  minio: { name: "MinIO", kind: "Infrastructure", source: compose },
  connect: { name: "Debezium", kind: "Infrastructure", source: connector },
  registry: { name: "Apicurio", kind: "Infrastructure", source: compose },
} as const;

export type NodeId = keyof typeof nodes;
export type Edge = { from: NodeId; to: NodeId; transport: string; source: string; optional?: boolean };

export const edges: readonly Edge[] = [
  { from: "web", to: "gateway", transport: "Connect RPC, WebSocket", source: nodes.web.source },
  { from: "gateway", to: "auth", transport: "Connect RPC, JWKS", source: gateway },
  { from: "gateway", to: "chat", transport: "Chat and Calls RPC, WebSocket", source: gateway },
  { from: "gateway", to: "game", transport: "Connect RPC, WebSocket", source: gateway },
  { from: "gateway", to: "tournament", transport: "Connect RPC", source: gateway },
  { from: "gateway", to: "analysis", transport: "Connect RPC", source: gateway },
  { from: "auth", to: "postgres", transport: "SQL, outbox", source: auth },
  { from: "auth", to: "redis", transport: "Redis", source: auth },
  { from: "chat", to: "postgres", transport: "SQL, outbox", source: chat },
  { from: "chat", to: "redis", transport: "Redis Pub/Sub", source: chat },
  { from: "chat", to: "kafka", transport: "Kafka producer", source: chat, optional: true },
  { from: "chat", to: "minio", transport: "S3-compatible attachment storage", source: chat },
  { from: "game", to: "postgres", transport: "SQL read model, leases, outbox", source: game },
  { from: "game", to: "kafka", transport: "Session journal, partition leases, rating and tournament consumers", source: game },
  { from: "tournament", to: "auth", transport: "Connect RPC, JWKS", source: tournament },
  { from: "tournament", to: "postgres", transport: "SQL, saga state, outbox", source: tournament },
  { from: "tournament", to: "kafka", transport: "game.finished.v1 and rating.changed.v1 consumer", source: tournament },
  { from: "analysis", to: "auth", transport: "JWKS", source: analysis },
  { from: "analysis", to: "postgres", transport: "SQL", source: analysis },
  { from: "analysis", to: "kafka", transport: "game.finished.v1 consumer", source: analysis },
  { from: "rating", to: "postgres", transport: "SQL, outbox", source: rating },
  { from: "rating", to: "redis", transport: "Redis leaderboard", source: rating },
  { from: "rating", to: "kafka", transport: "game.finished.v1 consumer", source: rating },
  { from: "connect", to: "postgres", transport: "Logical replication of outbox tables", source: connector },
  { from: "connect", to: "kafka", transport: "Outbox event router", source: connector },
];

export const layers = [
  ["web"],
  ["gateway"],
  ["auth", "chat", "game"],
  ["tournament", "analysis", "rating"],
  ["postgres", "redis", "kafka", "minio"],
  ["connect", "registry"],
] as const satisfies readonly (readonly NodeId[])[];
