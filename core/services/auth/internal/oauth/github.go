package oauth

import (
	"context"
	"net/http"
	"strconv"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// GitHub's API, kept as variables so tests can point them at a stand-in.
var (
	githubUserURL   = "https://api.github.com/user"
	githubEmailsURL = "https://api.github.com/user/emails"
)

// newGitHub builds the GitHub provider.
func newGitHub(clientID, clientSecret, redirectURL string) Provider {
	return &httpProvider{
		id: domain.ProviderGitHub,
		cfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     github.Endpoint,
			// read:user covers the profile; user:email is separate because
			// GitHub treats addresses as more sensitive than the rest — which is
			// precisely why the second request below is necessary.
			Scopes: []string{"read:user", "user:email"},
		},
		profile: githubProfile,
	}
}

// githubProfile reads the account, then its addresses.
//
// Two requests, because GET /user returns email: null for anyone who has kept
// their address private — which is a setting, not an edge case, and the people
// who set it are exactly the ones who would notice being unable to sign in.
// GET /user/emails is where the real answer lives, along with the verified flag
// that decides whether the address may be trusted at all.
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
		// Only the primary address. Any verified address would let someone with
		// a stale secondary address on their account sign in as a different
		// identity than the one they expect.
		if address.Primary {
			email, verified = address.Email, address.Verified
			break
		}
	}

	// GitHub's display name is optional; the login always exists and is what
	// their own UI falls back to.
	displayName := user.Name
	if displayName == "" {
		displayName = user.Login
	}

	var subject string
	if user.ID != 0 {
		// The numeric id, never the login: logins can be changed and reused by
		// someone else, and matching on one would eventually hand an account to
		// a stranger.
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
