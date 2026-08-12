package server

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"

	"github.com/bvivg/axon/core/services/auth/internal/avatar"
	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const tokenType = "Bearer"

func toProtoUser(u domain.User, avatarURLs avatar.URLBuilder) *authv1.User {
	out := &authv1.User{
		Id:            u.ID.String(),
		Email:         u.Email,
		EmailVerified: u.EmailVerified,
		CreatedAt:     timestamppb.New(u.CreatedAt),
	}

	if u.DisplayName != "" {
		out.DisplayName = &u.DisplayName
	}
	if u.FirstName != "" {
		out.FirstName = &u.FirstName
	}
	if u.LastName != "" {
		out.LastName = &u.LastName
	}
	if u.Nickname != "" {
		out.Nickname = &u.Nickname
	}
	if u.AvatarURL != "" {
		out.AvatarUrl = &u.AvatarURL

		urls := avatarURLs.URLs(u.ID)
		out.AvatarUrls = &authv1.AvatarURLs{
			Small:    urls.Small,
			Medium:   urls.Medium,
			Large:    urls.Large,
			Original: urls.Original,
		}
	}

	return out
}

func toProtoSession(s domain.Session) *authv1.Session {
	return &authv1.Session{
		Id:         s.FamilyID.String(),
		UserAgent:  s.UserAgent,
		Ip:         s.IP,
		StartedAt:  timestamppb.New(s.StartedAt),
		LastUsedAt: timestamppb.New(s.LastUsedAt),
		Current:    s.Current,
	}
}

func toProtoTokens(pair domain.TokenPair, now time.Time) *authv1.TokenPair {
	expiresIn := int64(pair.AccessExpiresAt.Sub(now).Seconds())
	if expiresIn < 0 {

		expiresIn = 0
	}

	return &authv1.TokenPair{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    expiresIn,
		TokenType:    tokenType,
	}
}

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
