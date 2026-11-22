package service_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

func appVerifier(t *testing.T) string {
	t.Helper()

	verifier, err := oauth.NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return verifier
}

func TestTheAppSignsInThroughItsOwnCallback(t *testing.T) {
	h := newHarness(t, withOAuth(t))
	verifier := appVerifier(t)

	started, err := h.svc.StartAppOAuth(t.Context(), domain.ProviderFake, oauth.Challenge(verifier), nil)
	if err != nil {
		t.Fatalf("StartAppOAuth: %v", err)
	}

	callback := h.callback(t, started.AuthorizationURL, identity("phone", "phone@fake.axon.test"))
	if !strings.HasSuffix(callback.Path, "/auth/callback/fake"+oauth.AppCallbackSuffix) {
		t.Fatalf("the provider returned to %s, want the app callback", callback.Path)
	}

	completed, err := h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider:    domain.ProviderFake,
		Code:        callback.Query().Get("code"),
		State:       callback.Query().Get("state"),
		AppVerifier: verifier,
	})
	if err != nil {
		t.Fatalf("CompleteOAuth: %v", err)
	}
	if !completed.Created || completed.User.Email != "phone@fake.axon.test" || completed.Tokens.AccessToken == "" {
		t.Errorf("completed = created %v, email %q, access token set %v", completed.Created, completed.User.Email, completed.Tokens.AccessToken != "")
	}
	if completed.ReturnTo != "" {
		t.Errorf("ReturnTo = %q, want none for the app", completed.ReturnTo)
	}
}

func TestAnAppStateIsUselessWithoutItsVerifier(t *testing.T) {
	h := newHarness(t, withOAuth(t))
	verifier := appVerifier(t)

	for name, presented := range map[string]string{
		"no verifier":      "",
		"another verifier": appVerifier(t),
		"the challenge":    oauth.Challenge(verifier),
	} {
		t.Run(name, func(t *testing.T) {
			started, err := h.svc.StartAppOAuth(t.Context(), domain.ProviderFake, oauth.Challenge(verifier), nil)
			if err != nil {
				t.Fatalf("StartAppOAuth: %v", err)
			}
			code := h.follow(t, started.AuthorizationURL, identity("stolen", "stolen@fake.axon.test"))

			_, err = h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
				Provider:    domain.ProviderFake,
				Code:        code,
				State:       started.State,
				AppVerifier: presented,
			})
			if !errors.Is(err, domain.ErrOauthStateInvalid) {
				t.Fatalf("CompleteOAuth = %v, want ErrOauthStateInvalid", err)
			}

			_, err = h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
				Provider:    domain.ProviderFake,
				Code:        code,
				State:       started.State,
				AppVerifier: verifier,
			})
			if !errors.Is(err, domain.ErrOauthStateInvalid) {
				t.Fatalf("a second attempt with the right verifier = %v, want the state already spent", err)
			}
		})
	}
}

func TestTheAppNeedsAnS256Challenge(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	for _, challenge := range []string{"", "plain-text", strings.Repeat("=", 43)} {
		if _, err := h.svc.StartAppOAuth(t.Context(), domain.ProviderFake, challenge, nil); !errors.Is(err, domain.ErrOauthChallengeInvalid) {
			t.Errorf("StartAppOAuth(%q) = %v, want ErrOauthChallengeInvalid", challenge, err)
		}
	}
}

func TestTheAppCannotUseAProviderWithoutAnAppCallback(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	if _, err := h.svc.StartAppOAuth(t.Context(), domain.ProviderApple, oauth.Challenge(appVerifier(t)), nil); !errors.Is(err, domain.ErrProviderUnsupported) {
		t.Errorf("StartAppOAuth(apple) = %v, want ErrProviderUnsupported", err)
	}
}

func TestTheAppLinksAProviderToTheSignedInAccount(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	owner := h.register(t, "phone.owner@example.test", validPassword)
	id := owner.User.ID
	verifier := appVerifier(t)

	started, err := h.svc.StartAppOAuth(t.Context(), domain.ProviderFake, oauth.Challenge(verifier), &id)
	if err != nil {
		t.Fatalf("StartAppOAuth: %v", err)
	}
	code := h.follow(t, started.AuthorizationURL, identity("phone-link", "phone.link@fake.axon.test"))

	linked, err := h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider:    domain.ProviderFake,
		Code:        code,
		State:       started.State,
		AppVerifier: verifier,
		CallerID:    &id,
	})
	if err != nil {
		t.Fatalf("CompleteOAuth: %v", err)
	}
	if !linked.Linked || linked.User.ID != id || linked.Tokens.AccessToken != "" {
		t.Errorf("linked = %v, user %s, access token set %v", linked.Linked, linked.User.ID, linked.Tokens.AccessToken != "")
	}

	providers, err := h.svc.LinkedProviders(t.Context(), id)
	if err != nil {
		t.Fatalf("LinkedProviders: %v", err)
	}
	if len(providers) != 1 || providers[0] != domain.ProviderFake {
		t.Errorf("linked providers = %v", providers)
	}
}

func TestAnAppLinkNeedsBothTheVerifierAndTheOwner(t *testing.T) {
	stranger := uuid.New()

	for name, tc := range map[string]struct {
		verifier func(string) string
		caller   func(uuid.UUID) *uuid.UUID
	}{
		"no verifier":     {verifier: func(string) string { return "" }, caller: func(id uuid.UUID) *uuid.UUID { return &id }},
		"no caller":       {verifier: func(v string) string { return v }, caller: func(uuid.UUID) *uuid.UUID { return nil }},
		"another account": {verifier: func(v string) string { return v }, caller: func(uuid.UUID) *uuid.UUID { return &stranger }},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, withOAuth(t))

			owner := h.register(t, "phone.owner@example.test", validPassword)
			id := owner.User.ID
			verifier := appVerifier(t)

			started, err := h.svc.StartAppOAuth(t.Context(), domain.ProviderFake, oauth.Challenge(verifier), &id)
			if err != nil {
				t.Fatalf("StartAppOAuth: %v", err)
			}
			code := h.follow(t, started.AuthorizationURL, identity("intruder", "intruder@fake.axon.test"))

			_, err = h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
				Provider:    domain.ProviderFake,
				Code:        code,
				State:       started.State,
				AppVerifier: tc.verifier(verifier),
				CallerID:    tc.caller(id),
			})
			if !errors.Is(err, domain.ErrOauthStateInvalid) {
				t.Fatalf("CompleteOAuth = %v, want ErrOauthStateInvalid", err)
			}

			providers, err := h.svc.LinkedProviders(t.Context(), id)
			if err != nil {
				t.Fatalf("LinkedProviders: %v", err)
			}
			if len(providers) != 0 {
				t.Errorf("linked providers = %v, want none", providers)
			}
		})
	}
}
