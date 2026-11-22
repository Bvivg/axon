import { createClient } from "@connectrpc/connect";

import { AnalysisAdminService } from "@/gen/axon/analysis/v1/admin_pb";
import { AuthAdminService } from "@/gen/axon/auth/v1/admin_pb";
import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { CallsAdminService } from "@/gen/axon/calls/v1/admin_pb";
import { ChatAdminService } from "@/gen/axon/chat/v1/admin_pb";
import { GameAdminService } from "@/gen/axon/game/v1/admin_pb";
import { RatingAdminService } from "@/gen/axon/rating/v1/admin_pb";
import { TournamentAdminService } from "@/gen/axon/tournament/v1/admin_pb";
import { TournamentService } from "@/gen/axon/tournament/v1/tournament_pb";
import { authenticate } from "@/lib/connect/interceptors";
import { bareTransport, newTransport } from "@/lib/connect/transport";

const transport = newTransport([authenticate]);

export const authClient = createClient(AuthService, bareTransport);
export const apiClient = createClient(AuthService, transport);
export const tournamentClient = createClient(TournamentService, transport);
export const authAdminClient = createClient(AuthAdminService, transport);
export const gameAdminClient = createClient(GameAdminService, transport);
export const chatAdminClient = createClient(ChatAdminService, transport);
export const callsAdminClient = createClient(CallsAdminService, transport);
export const ratingAdminClient = createClient(RatingAdminService, transport);
export const tournamentAdminClient = createClient(TournamentAdminService, transport);
export const analysisAdminClient = createClient(AnalysisAdminService, transport);
