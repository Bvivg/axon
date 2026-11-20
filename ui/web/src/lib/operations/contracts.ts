import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { CallsService } from "@/gen/axon/calls/v1/calls_pb";
import { ChatService } from "@/gen/axon/chat/v1/chat_pb";
import { GameService } from "@/gen/axon/game/v1/game_pb";

export const publicServices = [AuthService, ChatService, CallsService, GameService];

export const rpcRoutes = publicServices.flatMap((service) =>
  service.methods.map((method) => ({
    service: service.name,
    method: method.name,
    path: `/${service.typeName}/${method.name}`,
  })),
);
