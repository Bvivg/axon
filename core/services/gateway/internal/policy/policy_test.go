package policy_test

import (
	"testing"

	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"

	"github.com/bvivg/axon/core/services/gateway/internal/policy"
)

// The closed default is the whole point: adding an RPC and forgetting this file
// must make it unreachable, not public.
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
		"refresh":  {authv1connect.AuthServiceRefreshTokenProcedure, true, policy.TierSensitive},
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

// Sign-in and registration must never share the ordinary budget: one large
// enough for normal browsing is large enough to grind a password list.
func TestCredentialEndpointsAreOnTheTightBudget(t *testing.T) {
	for _, procedure := range []string{
		authv1connect.AuthServiceLoginProcedure,
		authv1connect.AuthServiceRegisterProcedure,
		authv1connect.AuthServiceRefreshTokenProcedure,
	} {
		if policy.For(procedure).Tier != policy.TierSensitive {
			t.Errorf("%s is not on the sensitive tier", procedure)
		}
	}
}

// Every procedure named here has to exist in the generated contract, or the
// table is protecting something that no longer has that name.
func TestPolicyMatchesTheContract(t *testing.T) {
	known := map[string]bool{
		authv1connect.AuthServiceRegisterProcedure:      true,
		authv1connect.AuthServiceLoginProcedure:         true,
		authv1connect.AuthServiceRefreshTokenProcedure:  true,
		authv1connect.AuthServiceLogoutProcedure:        true,
		authv1connect.AuthServiceGetMeProcedure:         true,
		authv1connect.AuthServiceStartOAuthProcedure:    true,
		authv1connect.AuthServiceCompleteOAuthProcedure: true,

		chatv1connect.ChatServiceCreateRoomProcedure:   true,
		chatv1connect.ChatServiceListRoomsProcedure:    true,
		chatv1connect.ChatServiceGetRoomProcedure:      true,
		chatv1connect.ChatServiceJoinRoomProcedure:     true,
		chatv1connect.ChatServiceLeaveRoomProcedure:    true,
		chatv1connect.ChatServiceListMessagesProcedure: true,
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
