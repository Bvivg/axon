import { createClient } from "@connectrpc/connect";

import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { ChatService } from "@/gen/axon/chat/v1/chat_pb";
import { authenticate } from "@/lib/connect/interceptors";
import { bareTransport, newTransport } from "@/lib/connect/transport";

/**
 * authClient is for the procedures that establish a session: Register, Login,
 * Logout, StartOAuth, CompleteOAuth. None of them takes an access token, and
 * none should be retried on one expiring.
 */
export const authClient = createClient(AuthService, bareTransport);

/**
 * apiClient is for calls made as a signed-in user — GetMe today, and every chat
 * and game procedure once those exist. It attaches the access token and
 * refreshes it when it has expired.
 */
export const apiClient = createClient(AuthService, newTransport([authenticate]));

/**
 * chatClient is rooms, membership and history. Everything a client waits for
 * rather than asks for — messages arriving — is on the socket instead; see
 * lib/ws/use-chat-socket.
 */
export const chatClient = createClient(ChatService, newTransport([authenticate]));
