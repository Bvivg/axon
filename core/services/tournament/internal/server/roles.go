package server

import (
	"context"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"
)

type AuthModerators struct {
	cache *authz.Cache
}

func NewAuthModerators(cache *authz.Cache) *AuthModerators {
	return &AuthModerators{cache: cache}
}

func (a *AuthModerators) Moderator(ctx context.Context, user uuid.UUID, authorization string) (bool, error) {
	set, err := a.cache.Permissions(ctx, user, authorization)
	if err != nil {
		return false, err
	}
	return set.Has(authz.GamesModerate), nil
}
