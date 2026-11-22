package service_test

import (
	"errors"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

func (h *harness) tryLink(t *testing.T, owner uuid.UUID, caller *uuid.UUID, who url.Values) (service.CompleteOAuthResult, error) {
	t.Helper()

	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderGoogle, webOrigin+"/profile/sessions", &owner)
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	return h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider: domain.ProviderGoogle,
		Code:     scriptedCode(t, who),
		State:    started.State,
		CallerID: caller,
	})
}

func TestTheOwnerLinksAProviderToTheirPasswordAccount(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	owner := h.register(t, "owner@example.test", validPassword)
	id := owner.User.ID

	linked, err := h.tryLink(t, id, &id, identity("owner-google", "owner.elsewhere@example.test"))
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if !linked.Linked {
		t.Error("the result does not report the provider as linked")
	}
	if linked.Tokens.AccessToken != "" || linked.Tokens.RefreshToken != "" {
		t.Error("linking issued a new session")
	}
	if linked.User.ID != id {
		t.Errorf("linked user = %s, want %s", linked.User.ID, id)
	}
	if linked.ReturnTo != webOrigin+"/profile/sessions" {
		t.Errorf("return to = %q", linked.ReturnTo)
	}

	providers, err := h.svc.LinkedProviders(t.Context(), id)
	if err != nil {
		t.Fatalf("LinkedProviders: %v", err)
	}
	if !slices.Equal(providers, []domain.Provider{domain.ProviderGoogle}) {
		t.Errorf("linked providers = %v", providers)
	}

	signedIn := h.signIn(t, identity("owner-google", "owner.elsewhere@example.test"))
	if signedIn.User.ID != id || signedIn.Created {
		t.Errorf("signing in with the linked provider reached %s (created %v), want %s", signedIn.User.ID, signedIn.Created, id)
	}
}

func TestLinkingCanOnlyBeCompletedByTheAccountThatStartedIt(t *testing.T) {
	stranger := uuid.New()

	for name, caller := range map[string]*uuid.UUID{
		"no caller":       nil,
		"another account": &stranger,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, withOAuth(t))

			owner := h.register(t, "owner@example.test", validPassword)

			_, err := h.tryLink(t, owner.User.ID, caller, identity("attacker-google", "attacker@example.test"))
			if !errors.Is(err, domain.ErrOauthStateInvalid) {
				t.Fatalf("err = %v, want ErrOauthStateInvalid", err)
			}

			if _, err := h.store.OauthAccountByProviderID(t.Context(), domain.ProviderGoogle, "attacker-google"); err == nil {
				t.Error("a provider identity was linked by someone other than the account owner")
			}
		})
	}
}

func TestLinkingRefusesAnIdentityThatBelongsToAnotherAccount(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	other := h.signIn(t, identity("shared-google", "other@example.test"))
	owner := h.register(t, "owner@example.test", validPassword)
	id := owner.User.ID

	_, err := h.tryLink(t, id, &id, identity("shared-google", "other@example.test"))
	if !errors.Is(err, domain.ErrOauthIdentityClaimed) {
		t.Fatalf("err = %v, want ErrOauthIdentityClaimed", err)
	}

	account, err := h.store.OauthAccountByProviderID(t.Context(), domain.ProviderGoogle, "shared-google")
	if err != nil || account.UserID != other.User.ID {
		t.Errorf("the identity moved away from its account: %+v, %v", account, err)
	}
}

func TestLinkingTheSameIdentityAgainIsHarmless(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	owner := h.register(t, "owner@example.test", validPassword)
	id := owner.User.ID

	for range 2 {
		if _, err := h.tryLink(t, id, &id, identity("owner-google", "owner@example.test")); err != nil {
			t.Fatalf("link: %v", err)
		}
	}

	providers, err := h.svc.LinkedProviders(t.Context(), id)
	if err != nil {
		t.Fatalf("LinkedProviders: %v", err)
	}
	if !slices.Equal(providers, []domain.Provider{domain.ProviderGoogle}) {
		t.Errorf("linked providers = %v, want google once", providers)
	}
}

func TestAnAccountWithoutLinksListsNoProviders(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	owner := h.register(t, "owner@example.test", validPassword)

	providers, err := h.svc.LinkedProviders(t.Context(), owner.User.ID)
	if err != nil {
		t.Fatalf("LinkedProviders: %v", err)
	}
	if len(providers) != 0 {
		t.Errorf("linked providers = %v, want none", providers)
	}
}
