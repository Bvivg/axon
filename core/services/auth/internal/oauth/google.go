package oauth

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

var googleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

func newGoogle(clientID, clientSecret, redirectURL string) Provider {
	return &httpProvider{
		id: domain.ProviderGoogle,
		cfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     google.Endpoint,

			Scopes: []string{"openid", "email", "profile"},
		},
		profile: googleProfile,
	}
}

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
