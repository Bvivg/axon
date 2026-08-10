package server

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// tokenType is the only scheme issued. It travels in the response so clients
// build the Authorization header from what they were given rather than
// hard-coding it.
const tokenType = "Bearer"

// toProtoUser converts a domain user into its wire form. Nothing
// credential-shaped exists on domain.User, so there is no way for a hash to
// leak through here by accident.
func toProtoUser(u domain.User) *authv1.User {
	out := &authv1.User{
		Id:            u.ID.String(),
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		CreatedAt:     timestamppb.New(u.CreatedAt),
	}

	// Empty means unset in the domain; on the wire it is an absent optional, so
	// a client can tell "no display name" from "display name is blank".
	if u.DisplayName != "" {
		out.DisplayName = &u.DisplayName
	}
	if u.AvatarURL != "" {
		out.AvatarUrl = &u.AvatarURL
	}

	return out
}

// toProtoTokens converts a token pair, turning the absolute expiry into the
// seconds-remaining the contract exposes.
func toProtoTokens(pair domain.TokenPair, now time.Time) *authv1.TokenPair {
	expiresIn := int64(pair.AccessExpiresAt.Sub(now).Seconds())
	if expiresIn < 0 {
		// Should not happen, but a negative lifetime would make a client refresh
		// in a loop rather than once.
		expiresIn = 0
	}

	return &authv1.TokenPair{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    expiresIn,
		TokenType:    tokenType,
	}
}

// fromProtoProvider maps the wire enum onto the domain provider.
//
// An unrecognised value returns false rather than a zero provider: a new client
// sending a provider this build does not know must be refused, not silently
// treated as the first one in the enum.
func fromProtoProvider(p authv1.OauthProvider) (domain.Provider, bool) {
	switch p {
	case authv1.OauthProvider_OAUTH_PROVIDER_GOOGLE:
		return domain.ProviderGoogle, true
	case authv1.OauthProvider_OAUTH_PROVIDER_GITHUB:
		return domain.ProviderGitHub, true
	case authv1.OauthProvider_OAUTH_PROVIDER_APPLE:
		return domain.ProviderApple, true
	case authv1.OauthProvider_OAUTH_PROVIDER_FAKE:
		return domain.ProviderFake, true
	default:
		return "", false
	}
}
