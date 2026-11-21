package policy_test

import (
	"testing"

	"github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1/analysisv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1/callsv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1/tournamentv1connect"

	"github.com/bvivg/axon/core/services/gateway/internal/policy"
)

func TestUnlistedProceduresAreClosed(t *testing.T) {
	for _, procedure := range []string{
		"/axon.auth.v1.AuthService/DeleteEverything",
		"/axon.game.v1.GameService/ApplyMove",
		"",
		"/nonsense",
	} {
		rule := policy.For(procedure)

		if rule.Public {
			t.Errorf("procedure %q defaults to public", procedure)
		}
		if rule.Tier != policy.TierSensitive {
			t.Errorf("procedure %q defaults to the loose budget", procedure)
		}
	}
}

func TestAuthProcedures(t *testing.T) {
	tests := map[string]struct {
		procedure  string
		wantPublic bool
		wantTier   policy.Tier
	}{
		"register": {authv1connect.AuthServiceRegisterProcedure, true, policy.TierSensitive},
		"login":    {authv1connect.AuthServiceLoginProcedure, true, policy.TierSensitive},
		"refresh":  {authv1connect.AuthServiceRefreshTokenProcedure, true, policy.TierRenewal},
		"logout":   {authv1connect.AuthServiceLogoutProcedure, true, policy.TierStandard},
		"get me":   {authv1connect.AuthServiceGetMeProcedure, false, policy.TierStandard},
		"start oauth": {
			authv1connect.AuthServiceStartOAuthProcedure, true, policy.TierSensitive,
		},
		"complete oauth": {
			authv1connect.AuthServiceCompleteOAuthProcedure, true, policy.TierSensitive,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rule := policy.For(tt.procedure)

			if rule.Public != tt.wantPublic {
				t.Errorf("Public = %v, want %v", rule.Public, tt.wantPublic)
			}
			if rule.Tier != tt.wantTier {
				t.Errorf("Tier = %v, want %v", rule.Tier, tt.wantTier)
			}
		})
	}
}

func TestRBACProcedures(t *testing.T) {
	tests := map[string]struct {
		procedure  string
		wantPublic bool
		wantTier   policy.Tier
	}{
		"list roles":  {authv1connect.AuthServiceListRolesProcedure, false, policy.TierStandard},
		"assign role": {authv1connect.AuthServiceAssignRoleProcedure, false, policy.TierSensitive},
		"revoke role": {authv1connect.AuthServiceRevokeRoleProcedure, false, policy.TierSensitive},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rule := policy.For(tt.procedure)

			if rule.Public != tt.wantPublic {
				t.Errorf("Public = %v, want %v", rule.Public, tt.wantPublic)
			}
			if rule.Tier != tt.wantTier {
				t.Errorf("Tier = %v, want %v", rule.Tier, tt.wantTier)
			}
		})
	}
}

func TestDirectMessagingProcedures(t *testing.T) {
	tests := map[string]struct {
		procedure  string
		wantPublic bool
		wantTier   policy.Tier
	}{
		"search users":    {authv1connect.AuthServiceSearchUsersProcedure, false, policy.TierSearch},
		"search messages": {chatv1connect.ChatServiceSearchMessagesProcedure, false, policy.TierSearch},
		"game history":    {gamev1connect.GameServiceGetSessionHistoryProcedure, false, policy.TierSearch},
		"get users public profiles": {
			authv1connect.AuthServiceGetUsersPublicProfilesProcedure, false, policy.TierStandard,
		},
		"get public profile by username": {
			authv1connect.AuthServiceGetPublicProfileByUsernameProcedure, false, policy.TierStandard,
		},
		"update last seen": {authv1connect.AuthServiceUpdateLastSeenProcedure, false, policy.TierStandard},
		"hide room":        {chatv1connect.ChatServiceHideRoomProcedure, false, policy.TierStandard},
		"get direct room":  {chatv1connect.ChatServiceGetDirectRoomProcedure, false, policy.TierStandard},
		"mark read":        {chatv1connect.ChatServiceMarkReadProcedure, false, policy.TierStandard},
		"create group":     {chatv1connect.ChatServiceCreateGroupProcedure, false, policy.TierSensitive},
		"add members":      {chatv1connect.ChatServiceAddGroupMembersProcedure, false, policy.TierSensitive},
		"remove member":    {chatv1connect.ChatServiceRemoveGroupMemberProcedure, false, policy.TierStandard},
		"rename group":     {chatv1connect.ChatServiceRenameGroupProcedure, false, policy.TierStandard},
		"message readers":  {chatv1connect.ChatServiceListMessageReadersProcedure, false, policy.TierStandard},
		"forward messages": {chatv1connect.ChatServiceForwardMessagesProcedure, false, policy.TierSensitive},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rule := policy.For(tt.procedure)

			if rule.Public != tt.wantPublic {
				t.Errorf("Public = %v, want %v", rule.Public, tt.wantPublic)
			}
			if rule.Tier != tt.wantTier {
				t.Errorf("Tier = %v, want %v", rule.Tier, tt.wantTier)
			}
		})
	}
}

func TestTournamentProcedures(t *testing.T) {
	tests := map[string]struct {
		procedure string
		wantTier  policy.Tier
	}{
		"create":   {tournamentv1connect.TournamentServiceCreateTournamentProcedure, policy.TierSensitive},
		"start":    {tournamentv1connect.TournamentServiceStartTournamentProcedure, policy.TierSensitive},
		"register": {tournamentv1connect.TournamentServiceRegisterForTournamentProcedure, policy.TierStandard},
		"withdraw": {tournamentv1connect.TournamentServiceWithdrawFromTournamentProcedure, policy.TierStandard},
		"get":      {tournamentv1connect.TournamentServiceGetTournamentProcedure, policy.TierStandard},
		"list":     {tournamentv1connect.TournamentServiceListTournamentsProcedure, policy.TierStandard},
		"cancel":   {tournamentv1connect.TournamentServiceCancelTournamentProcedure, policy.TierStandard},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rule := policy.For(tt.procedure)

			if rule.Public {
				t.Error("a tournament procedure is public")
			}
			if rule.Tier != tt.wantTier {
				t.Errorf("Tier = %v, want %v", rule.Tier, tt.wantTier)
			}
		})
	}
}

func TestCredentialEndpointsAreOnTheTightBudget(t *testing.T) {
	for _, procedure := range []string{
		authv1connect.AuthServiceLoginProcedure,
		authv1connect.AuthServiceRegisterProcedure,
		authv1connect.AuthServiceStartOAuthProcedure,
		authv1connect.AuthServiceCompleteOAuthProcedure,
	} {
		if policy.For(procedure).Tier != policy.TierSensitive {
			t.Errorf("%s is not on the sensitive tier", procedure)
		}
	}
	if policy.For(authv1connect.AuthServiceRefreshTokenProcedure).Tier != policy.TierRenewal {
		t.Error("refresh is not on the renewal tier")
	}
}

func TestPolicyMatchesTheContract(t *testing.T) {
	known := map[string]bool{
		gamev1connect.GameServiceCreateSessionProcedure:       true,
		gamev1connect.GameServiceJoinSessionProcedure:         true,
		gamev1connect.GameServiceGetSessionProcedure:          true,
		gamev1connect.GameServiceListSessionsProcedure:        true,
		gamev1connect.GameServiceFindMatchProcedure:           true,
		gamev1connect.GameServiceCancelMatchProcedure:         true,
		gamev1connect.GameServiceGetMatchProcedure:            true,
		gamev1connect.GameServiceGetRatingsProcedure:          true,
		gamev1connect.GameServiceLeaveSessionProcedure:        true,
		gamev1connect.GameServiceGetSessionHistoryProcedure:   true,
		gamev1connect.GameServiceListLiveSessionsProcedure:    true,
		analysisv1connect.AnalysisServiceGetAnalysisProcedure: true,

		tournamentv1connect.TournamentServiceCreateTournamentProcedure:       true,
		tournamentv1connect.TournamentServiceStartTournamentProcedure:        true,
		tournamentv1connect.TournamentServiceRegisterForTournamentProcedure:  true,
		tournamentv1connect.TournamentServiceWithdrawFromTournamentProcedure: true,
		tournamentv1connect.TournamentServiceGetTournamentProcedure:          true,
		tournamentv1connect.TournamentServiceListTournamentsProcedure:        true,
		tournamentv1connect.TournamentServiceCancelTournamentProcedure:       true,

		callsv1connect.CallsServiceRegisterEndpointProcedure:        true,
		callsv1connect.CallsServiceStartCallProcedure:               true,
		callsv1connect.CallsServiceStartDirectCallProcedure:         true,
		callsv1connect.CallsServiceAcceptCallProcedure:              true,
		callsv1connect.CallsServiceDeclineCallProcedure:             true,
		callsv1connect.CallsServiceCancelCallProcedure:              true,
		callsv1connect.CallsServiceEndCallProcedure:                 true,
		callsv1connect.CallsServiceReportConnectionProcedure:        true,
		callsv1connect.CallsServiceAcknowledgeCleanupProcedure:      true,
		callsv1connect.CallsServiceGetCallProcedure:                 true,
		callsv1connect.CallsServiceGetActiveCallProcedure:           true,
		callsv1connect.CallsServiceListCallsProcedure:               true,
		callsv1connect.CallsServiceGetMediaCredentialsProcedure:     true,
		callsv1connect.CallsServiceGetCallCapabilitiesProcedure:     true,
		callsv1connect.CallsServiceStartGroupCallProcedure:          true,
		callsv1connect.CallsServiceStartGroupCallFromCallProcedure:  true,
		callsv1connect.CallsServiceInviteCallParticipantProcedure:   true,
		callsv1connect.CallsServiceLeaveCallProcedure:               true,
		callsv1connect.CallsServiceRemoveCallParticipantProcedure:   true,
		callsv1connect.CallsServiceGetSfuCredentialsProcedure:       true,
		callsv1connect.CallsServiceRequestCallMigrationProcedure:    true,
		callsv1connect.CallsServiceReportCallMigrationProcedure:     true,
		callsv1connect.CallsServiceGetMigrationCredentialsProcedure: true,
		authv1connect.AuthServiceRegisterProcedure:                  true,
		authv1connect.AuthServiceLoginProcedure:                     true,
		authv1connect.AuthServiceRefreshTokenProcedure:              true,
		authv1connect.AuthServiceLogoutProcedure:                    true,
		authv1connect.AuthServiceGetMeProcedure:                     true,
		authv1connect.AuthServiceStartOAuthProcedure:                true,
		authv1connect.AuthServiceCompleteOAuthProcedure:             true,
		authv1connect.AuthServiceGetUserByEmailProcedure:            true,
		authv1connect.AuthServiceListRolesProcedure:                 true,
		authv1connect.AuthServiceAssignRoleProcedure:                true,
		authv1connect.AuthServiceRevokeRoleProcedure:                true,

		authv1connect.AuthServiceSearchUsersProcedure:                true,
		authv1connect.AuthServiceGetUsersPublicProfilesProcedure:     true,
		authv1connect.AuthServiceGetPublicProfileByUsernameProcedure: true,
		authv1connect.AuthServiceUpdateLastSeenProcedure:             true,

		chatv1connect.ChatServiceCreateRoomProcedure:     true,
		chatv1connect.ChatServiceListRoomsProcedure:      true,
		chatv1connect.ChatServiceGetRoomProcedure:        true,
		chatv1connect.ChatServiceJoinRoomProcedure:       true,
		chatv1connect.ChatServiceLeaveRoomProcedure:      true,
		chatv1connect.ChatServiceListMessagesProcedure:   true,
		chatv1connect.ChatServiceHideRoomProcedure:       true,
		chatv1connect.ChatServiceGetDirectRoomProcedure:  true,
		chatv1connect.ChatServiceMarkReadProcedure:       true,
		chatv1connect.ChatServiceSearchMessagesProcedure: true,

		chatv1connect.ChatServiceCreateGroupProcedure:       true,
		chatv1connect.ChatServiceAddGroupMembersProcedure:   true,
		chatv1connect.ChatServiceRemoveGroupMemberProcedure: true,
		chatv1connect.ChatServiceRenameGroupProcedure:       true,

		chatv1connect.ChatServiceListMessageReadersProcedure: true,
		chatv1connect.ChatServiceForwardMessagesProcedure:    true,

		chatv1connect.ChatServiceGetPushKeyProcedure:      true,
		chatv1connect.ChatServiceSubscribePushProcedure:   true,
		chatv1connect.ChatServiceUnsubscribePushProcedure: true,
	}

	for _, procedure := range policy.Procedures() {
		if !known[procedure] {
			t.Errorf("policy names %q, which is not a procedure in the contract", procedure)
		}
	}

	if got, want := len(policy.Procedures()), len(known); got != want {
		t.Errorf("policy covers %d procedures, the contract has %d — a new RPC is unlisted", got, want)
	}
}
