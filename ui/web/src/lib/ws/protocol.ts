/**
 * The chat socket's frame vocabulary.
 *
 * Hand-written rather than generated, and that is not an oversight: the socket
 * protocol is deliberately outside the proto contract (see the note at the top
 * of chat.proto), so there is nothing to generate from. The other side of this
 * file is core/services/chat/internal/ws/protocol.go — the two are changed
 * together.
 */

/** Names the protocol and its version. The server refuses anything else. */
export const subprotocol = "axon.chat.v1";

/**
 * Carries the access token in the subprotocol list.
 *
 * A page cannot set an Authorization header on an upgrade — the WebSocket API
 * takes a URL and a list of protocol names and nothing else. The gateway takes
 * the token off this list and puts it in a real header on the hop inwards. The
 * alternatives are worse: a token in the query string lands in browser history
 * and in every access log along the way.
 */
export const bearerPrefix = "axon.bearer.";

/** Frames this client sends. */
export type Outgoing =
  | { type: "subscribe"; room_id: string; since?: number }
  | { type: "unsubscribe"; room_id: string }
  | { type: "send"; room_id: string; client_id: string; body: string };

/** A message as it arrives on the socket. */
export interface WireMessage {
  id: string;
  room_id: string;
  author_id: string;
  body: string;
  seq: number;
  sent_at: string;

  /** Present only on the reader's own messages, echoed back from the send. */
  client_id?: string;
}

/** Frames the server sends. */
export type Incoming =
  | { type: "message"; message: WireMessage }
  | { type: "subscribed"; room_id: string; seq: number }
  | { type: "ack"; room_id: string; client_id: string; seq: number }
  | { type: "error"; code: string; reason: string; client_id?: string };

/** Close codes the server uses. Above 4000 is the application's own range. */
export const closeCodes = {
  /** The socket arrived without a usable token. */
  unauthenticated: 4401,
  /**
   * The token that opened the socket has run out. Not a failure: the client
   * refreshes and reconnects from the position it already has.
   */
  tokenExpired: 4402,
  /** A frame the protocol does not define — a bug on this side. */
  protocol: 4400,
} as const;

/** parseFrame reads a frame, returning null for anything unreadable. */
export function parseFrame(data: string): Incoming | null {
  try {
    const frame = JSON.parse(data) as Incoming;
    return typeof frame?.type === "string" ? frame : null;
  } catch {
    return null;
  }
}

/**
 * newClientID makes the id a sent message is deduplicated by.
 *
 * crypto.randomUUID is a secure-context API and simply does not exist
 * otherwise — no flag, no polyfill path, a plain TypeError on the first
 * message. Plain HTTP is not a corner case here: it is what fronts this
 * client whenever the deployment is not yet on HTTPS, this suite's own stack
 * included (see the header of docker-compose.web-e2e.yml). The id only has to
 * be unique per sender, not unguessable, so Math.random is an honest fallback
 * rather than a weakened security control.
 */
export function newClientID(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}
