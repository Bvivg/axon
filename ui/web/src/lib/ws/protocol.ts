export const subprotocol = "axon.chat.v1";

export const bearerPrefix = "axon.bearer.";

export type Outgoing =
  | { type: "subscribe"; room_id: string; since?: number }
  | { type: "unsubscribe"; room_id: string }
  | { type: "send"; room_id: string; client_id: string; body: string };

export interface WireMessage {
  id: string;
  room_id: string;
  author_id: string;
  body: string;
  seq: number;
  sent_at: string;

  client_id?: string;
}

export type Incoming =
  | { type: "message"; message: WireMessage }
  | { type: "subscribed"; room_id: string; seq: number }
  | { type: "ack"; room_id: string; client_id: string; seq: number }
  | { type: "error"; code: string; reason: string; client_id?: string };

export const closeCodes = {

  unauthenticated: 4401,

  tokenExpired: 4402,

  protocol: 4400,
} as const;

export function parseFrame(data: string): Incoming | null {
  try {
    const frame = JSON.parse(data) as Incoming;
    return typeof frame?.type === "string" ? frame : null;
  } catch {
    return null;
  }
}

export function newClientID(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}
