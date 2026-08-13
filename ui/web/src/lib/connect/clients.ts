import { createClient } from "@connectrpc/connect";

import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { ChatService } from "@/gen/axon/chat/v1/chat_pb";
import { authenticate } from "@/lib/connect/interceptors";
import { bareTransport, newTransport } from "@/lib/connect/transport";

export const authClient = createClient(AuthService, bareTransport);

export const apiClient = createClient(AuthService, newTransport([authenticate]));

export const chatClient = createClient(ChatService, newTransport([authenticate]));
