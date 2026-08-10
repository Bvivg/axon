package oauth

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// googleUserInfoURL is the OpenID Connect userinfo endpoint.
//
// The same claims arrive inside the id_token in the exchange response, and
// reading them from there would save a round trip. It would also mean verifying
// Google's signature, tracking their key rotation and handling the failure modes
// of both — for a request that is already inside a request the user is waiting
// on. The endpoint is the boring choice and needs no key management.
var googleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

// newGoogle builds the Google provider.
func newGoogle(clientID, clientSecret, redirectURL string) Provider {
	return &httpProvider{
		id: domain.ProviderGoogle,
		cfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     google.Endpoint,
			// Nothing beyond identity. Asking for more would show up on the
			// consent screen as a reason not to sign in.
			Scopes: []string{"openid", "email", "profile"},
		},
		profile: googleProfile,
	}
}

// googleProfile reads the OpenID Connect claims.
func googleProfile(ctx context.Context, client *http.Client) (domain.ProviderProfile, error) {
	var body struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}

	if err := get(ctx, client, googleUserInfoURL, &body); err != nil {
		return domain.ProviderProfile{}, err
	}

	return domain.ProviderProfile{
		ProviderUserID: body.Subject,
		Email:          body.Email,
		EmailVerified:  body.EmailVerified,
		DisplayName:    body.Name,
		AvatarURL:      body.Picture,
	}, nil
}
