export const subprotocol = "axon.chat.v1";

export const bearerPrefix = "axon.bearer.";

export type Outgoing =
  | { type: "subscribe"; room_id: string; since?: number }
  | { type: "unsubscribe"; room_id: string }
  | { type: "typing"; room_id: string }
  | { type: "watch_presence"; to_user_id: string }
  | { type: "edit"; client_id: string; message_id: string; body: string }
  | { type: "delete"; message_id: string }
  | ({
      type: "send";
      client_id: string;
      body: string;
      upload_id?: string;
      reply_to_id?: string;
      forwarded_from_id?: string;
    } & ({ room_id: string } | { to_user_id: string }));

export interface WireReplyPreview {
  id: string;
  author_id: string;
  kind: string;
  body: string;
  deleted?: boolean;
}

export interface WireMessage {
  id: string;
  room_id: string;
  author_id: string;
  body: string;
  seq: number;
  sent_at: string;

  client_id?: string;

  kind?: string;

  payload?: Record<string, unknown>;

  reply_to_id?: string;
  reply_to?: WireReplyPreview;

  forwarded_from_id?: string;
  forwarded_from_author_id?: string;

  edited_at?: string;
  deleted_at?: string;
}

export type Incoming =
  | { type: "message"; message: WireMessage }
  | { type: "subscribed"; room_id: string; seq: number }
  | { type: "ack"; room_id: string; client_id: string; seq: number }
  | { type: "read"; room_id: string; user_id: string; seq: number }
  | { type: "typing"; room_id: string; user_id: string }
  | { type: "presence"; user_id: string }
  | { type: "message_updated"; message: WireMessage }
  | { type: "room_added"; room_id: string }
  | { type: "room_removed"; room_id: string }
  | { type: "error"; code: string; reason: string; client_id?: string };

export const closeCodes = {

  unauthenticated: 4401,

  tokenExpired: 4402,

  protocol: 4400,

  sessionRevoked: 4403,
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
