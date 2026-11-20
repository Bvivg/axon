import { createClient } from "@connectrpc/connect";

import { AnalysisService } from "@/gen/axon/analysis/v1/analysis_pb";
import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { ChatService } from "@/gen/axon/chat/v1/chat_pb";
import { CallsService } from "@/gen/axon/calls/v1/calls_pb";
import { GameService } from "@/gen/axon/game/v1/game_pb";
import { TournamentService } from "@/gen/axon/tournament/v1/tournament_pb";
import { authenticate } from "@/lib/connect/interceptors";
import { bareTransport, newTransport } from "@/lib/connect/transport";

export const authClient = createClient(AuthService, bareTransport);

export const apiClient = createClient(AuthService, newTransport([authenticate]));

export const chatClient = createClient(ChatService, newTransport([authenticate]));
export const callsClient = createClient(CallsService, newTransport([authenticate]));
export const gameClient = createClient(GameService, newTransport([authenticate]));
export const analysisClient = createClient(AnalysisService, newTransport([authenticate]));
export const tournamentClient = createClient(TournamentService, newTransport([authenticate]));
