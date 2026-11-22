//go:build e2e

package e2e

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

func appPKCE(t *testing.T) (verifier, challenge string) {
	t.Helper()

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func (c *caller) startAppOAuth(t *testing.T, challenge string) *authv1.StartOAuthResponse {
	t.Helper()

	resp, err := c.client.StartOAuth(t0(), connect.NewRequest(&authv1.StartOAuthRequest{
		Provider:     authv1.OauthProvider_OAUTH_PROVIDER_FAKE,
		AppChallenge: &challenge,
	}))
	if err != nil {
		t.Fatalf("StartOAuth for the app: %v", err)
	}
	return resp.Msg
}

func (c *caller) completeAppOAuth(code, state, verifier string) (*authv1.CompleteOAuthResponse, error) {
	resp, err := c.client.CompleteOAuth(t0(), connect.NewRequest(&authv1.CompleteOAuthRequest{
		Provider:    authv1.OauthProvider_OAUTH_PROVIDER_FAKE,
		Code:        code,
		State:       state,
		AppVerifier: &verifier,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func TestTheAppSignsInWithItsVerifier(t *testing.T) {
	c := newCaller(t)
	verifier, challenge := appPKCE(t)
	subject := "e2e-app-" + uuid.NewString()

	started := c.startAppOAuth(t, challenge)
	if !strings.Contains(started.GetAuthorizationUrl(), "%2Ffake%2Fapp") {
		t.Fatalf("the authorization URL does not return to the app callback: %s", started.GetAuthorizationUrl())
	}

	code, state := consent(t, rewriteForRunner(t, started.GetAuthorizationUrl()), fakeIdentity(subject, subject+"@fake.axon.test"))

	result, err := c.completeAppOAuth(code, state, verifier)
	if err != nil {
		t.Fatalf("CompleteOAuth with the verifier: %v", err)
	}
	if result.GetTokens().GetRefreshToken() == "" || result.GetTokens().GetAccessToken() == "" {
		t.Fatal("the app got no token pair")
	}

	user, err := c.getMe(result.GetTokens().GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe with the app token: %v", err)
	}
	if user.GetEmail() != subject+"@fake.axon.test" {
		t.Errorf("email = %q", user.GetEmail())
	}
}

func TestAnInterceptedAppCallbackCannotSignIn(t *testing.T) {
	c := newCaller(t)
	_, challenge := appPKCE(t)
	stolen, _ := appPKCE(t)
	subject := "e2e-app-" + uuid.NewString()

	started := c.startAppOAuth(t, challenge)
	code, state := consent(t, rewriteForRunner(t, started.GetAuthorizationUrl()), fakeIdentity(subject, subject+"@fake.axon.test"))

	if _, err := c.completeOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, code, state, ""); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("completing without a verifier = %v, want invalid_argument", connect.CodeOf(err))
	}

	started = c.startAppOAuth(t, challenge)
	code, state = consent(t, rewriteForRunner(t, started.GetAuthorizationUrl()), fakeIdentity(subject, subject+"@fake.axon.test"))

	if _, err := c.completeAppOAuth(code, state, stolen); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("completing with another verifier = %v, want invalid_argument", connect.CodeOf(err))
	}
}

func (c *caller) startAppLink(accessToken, challenge string) (*authv1.StartOAuthResponse, error) {
	link := true
	req := connect.NewRequest(&authv1.StartOAuthRequest{
		Provider:     authv1.OauthProvider_OAUTH_PROVIDER_FAKE,
		AppChallenge: &challenge,
		Link:         &link,
	})
	if accessToken != "" {
		req.Header().Set(authn.Header, "Bearer "+accessToken)
	}
	resp, err := c.client.StartOAuth(t0(), req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c *caller) completeAppLink(accessToken, code, state, verifier string) (*authv1.CompleteOAuthResponse, error) {
	req := connect.NewRequest(&authv1.CompleteOAuthRequest{
		Provider:    authv1.OauthProvider_OAUTH_PROVIDER_FAKE,
		Code:        code,
		State:       state,
		AppVerifier: &verifier,
	})
	req.Header().Set(authn.Header, "Bearer "+accessToken)
	resp, err := c.client.CompleteOAuth(t0(), req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func TestTheAppLinksAProviderToTheSignedInAccount(t *testing.T) {
	c := newCaller(t)
	owner := c.register(t)
	verifier, challenge := appPKCE(t)
	subject := "e2e-app-link-" + uuid.NewString()

	if _, err := c.startAppLink("", challenge); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("starting an app link without a session = %v, want unauthenticated", connect.CodeOf(err))
	}

	started, err := c.startAppLink(owner.tokens.GetAccessToken(), challenge)
	if err != nil {
		t.Fatalf("StartOAuth to link from the app: %v", err)
	}
	if !strings.Contains(started.GetAuthorizationUrl(), "%2Ffake%2Fapp") {
		t.Fatalf("the link does not return to the app callback: %s", started.GetAuthorizationUrl())
	}

	code, state := consent(t, rewriteForRunner(t, started.GetAuthorizationUrl()), fakeIdentity(subject, subject+"@fake.axon.test"))
	linked, err := c.completeAppLink(owner.tokens.GetAccessToken(), code, state, verifier)
	if err != nil {
		t.Fatalf("CompleteOAuth to link from the app: %v", err)
	}
	if !linked.GetLinked() || linked.GetTokens() != nil || linked.GetUser().GetId() != owner.userID {
		t.Fatalf("linked = %v, tokens issued %v, user %s", linked.GetLinked(), linked.GetTokens() != nil, linked.GetUser().GetId())
	}

	req := connect.NewRequest(&authv1.GetMeRequest{})
	req.Header().Set(authn.Header, "Bearer "+owner.tokens.GetAccessToken())
	me, err := c.client.GetMe(t0(), req)
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if got := me.Msg.GetLinkedProviders(); len(got) != 1 || got[0] != authv1.OauthProvider_OAUTH_PROVIDER_FAKE {
		t.Errorf("linked providers = %v, want the fake provider", got)
	}
}
