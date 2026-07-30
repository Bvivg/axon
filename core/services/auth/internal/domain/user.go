// Package domain holds the auth service's entities and the errors it can fail
// with. It knows nothing about Postgres, Connect or HTTP: the repository and the
// server layers depend on it, never the other way round.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// User is an account. It never carries credentials — those live in Credential,
// so a user can be loaded and returned without a password hash ever being read.
type User struct {
	ID            uuid.UUID
	Email         string
	EmailVerified bool

	// DisplayName and AvatarURL are empty when unset. OAuth sign-in fills them
	// in from the provider's profile.
	DisplayName string
	AvatarURL   string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Credential is a user's password. It is a separate entity because a
// user who only ever signed in through a provider has no password at all,
// and a nullable column on users would make that state easy to overlook.
type Credential struct {
	UserID uuid.UUID

	// PasswordHash is a full PHC-format argon2id string, parameters included.
	PasswordHash string

	UpdatedAt time.Time
}

// Provider identifies an external identity provider.
type Provider string

const (
	ProviderGoogle Provider = "google"
	ProviderGitHub Provider = "github"
	ProviderApple  Provider = "apple"

	// ProviderFake runs the full authorization code flow locally. It is refused
	// outside development and test, so a production deployment cannot be talked
	// into accepting an identity nobody vouched for.
	ProviderFake Provider = "fake"
)

// Valid reports whether p is a provider the service knows.
func (p Provider) Valid() bool {
	switch p {
	case ProviderGoogle, ProviderGitHub, ProviderApple, ProviderFake:
		return true
	default:
		return false
	}
}

func (p Provider) String() string { return string(p) }

// OauthAccount links a user to an identity at an external provider. The pair
// (Provider, ProviderUserID) is unique: two people cannot claim one provider
// identity, and one person signing in twice resolves to the same account.
//
// ProviderUserID is the provider's stable subject identifier, never the email —
// people change their email at the provider, and matching on it would hand an
// account to whoever claims the address next.
type OauthAccount struct {
	Provider       Provider
	ProviderUserID string
	UserID         uuid.UUID
	Email          string
	LinkedAt       time.Time
}

// ProviderProfile is what a provider tells us about the person signing in.
type ProviderProfile struct {
	// ProviderUserID is the provider's stable subject identifier.
	ProviderUserID string
	Email          string
	EmailVerified  bool
	DisplayName    string
	AvatarURL      string
}
