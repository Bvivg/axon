package oauth

import (
	"context"
	"net/http"
	"strconv"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

var (
	githubUserURL   = "https://api.github.com/user"
	githubEmailsURL = "https://api.github.com/user/emails"
)

func newGitHub(clientID, clientSecret, redirectURL string) Provider {
	return &httpProvider{
		id: domain.ProviderGitHub,
		cfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     github.Endpoint,

			Scopes: []string{"read:user", "user:email"},
		},
		profile: githubProfile,
	}
}

func githubProfile(ctx context.Context, client *http.Client) (domain.ProviderProfile, error) {
	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}

	if err := get(ctx, client, githubUserURL, &user); err != nil {
		return domain.ProviderProfile{}, err
	}

	var addresses []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}

	if err := get(ctx, client, githubEmailsURL, &addresses); err != nil {
		return domain.ProviderProfile{}, err
	}

	var email string
	var verified bool
	for _, address := range addresses {

		if address.Primary {
			email, verified = address.Email, address.Verified
			break
		}
	}

	displayName := user.Name
	if displayName == "" {
		displayName = user.Login
	}

	var subject string
	if user.ID != 0 {

		subject = strconv.FormatInt(user.ID, 10)
	}

	return domain.ProviderProfile{
		ProviderUserID: subject,
		Email:          email,
		EmailVerified:  verified,
		DisplayName:    displayName,
		AvatarURL:      user.AvatarURL,
	}, nil
}
