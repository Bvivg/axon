import { AuthAdminService } from "@/gen/axon/auth/v1/admin_pb";
import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { CallsAdminService } from "@/gen/axon/calls/v1/admin_pb";
import { CallsService } from "@/gen/axon/calls/v1/calls_pb";
import { ChatAdminService } from "@/gen/axon/chat/v1/admin_pb";
import { ChatService } from "@/gen/axon/chat/v1/chat_pb";
import { GameAdminService } from "@/gen/axon/game/v1/admin_pb";
import { GameService } from "@/gen/axon/game/v1/game_pb";
import { RatingAdminService } from "@/gen/axon/rating/v1/admin_pb";

export const publicServices = [AuthService, ChatService, CallsService, GameService];

export const adminServices = [AuthAdminService, GameAdminService, ChatAdminService, CallsAdminService, RatingAdminService];

export const rpcRoutes = [...publicServices, ...adminServices].flatMap((service) =>
  service.methods.map((method) => ({
    service: service.name,
    method: method.name,
    path: `/${service.typeName}/${method.name}`,
  })),
);
