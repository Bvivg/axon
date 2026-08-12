package domain

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID            uuid.UUID
	Email         string
	EmailVerified bool

	DisplayName string
	AvatarURL   string

	FirstName string
	LastName  string
	Nickname  string

	AvatarIsCustom bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

type ProfilePatch struct {
	Email     *string
	FirstName *string
	LastName  *string
	Nickname  *string
}

type ProfileUpdate struct {
	Email         string
	EmailVerified bool
	FirstName     string
	LastName      string
	Nickname      string
	DisplayName   string
}

type Credential struct {
	UserID uuid.UUID

	PasswordHash string

	UpdatedAt time.Time
}

type Provider string

const (
	ProviderGoogle Provider = "google"
	ProviderGitHub Provider = "github"
	ProviderApple  Provider = "apple"

	ProviderFake Provider = "fake"
)

func (p Provider) Valid() bool {
	switch p {
	case ProviderGoogle, ProviderGitHub, ProviderApple, ProviderFake:
		return true
	default:
		return false
	}
}

func (p Provider) String() string { return string(p) }

type OauthAccount struct {
	Provider       Provider
	ProviderUserID string
	UserID         uuid.UUID
	Email          string
	LinkedAt       time.Time
}

type ProviderProfile struct {
	ProviderUserID string
	Email          string
	EmailVerified  bool
	DisplayName    string
	AvatarURL      string

	IsPrivateEmail bool
}
