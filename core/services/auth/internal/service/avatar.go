package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

var ErrAvatarStorageUnavailable = errors.New("service: avatar storage is not configured")

func (s *Service) UploadAvatar(ctx context.Context, userID uuid.UUID, raw []byte) (domain.User, error) {
	if s.avatar == nil {
		return domain.User{}, ErrAvatarStorageUnavailable
	}

	urls, err := s.avatar.Process(ctx, userID, raw)
	if err != nil {
		return domain.User{}, err
	}

	return s.store.SetAvatar(ctx, userID, urls.Medium, true)
}

func (s *Service) importProviderAvatar(ctx context.Context, user domain.User, sourceURL string) domain.User {
	if s.avatar == nil || sourceURL == "" || user.AvatarIsCustom {
		return user
	}

	urls, err := s.avatar.ProcessFromURL(ctx, user.ID, sourceURL)
	if err != nil {
		s.log.WarnContext(ctx, "could not import the provider avatar", "user_id", user.ID, "error", err)
		return user
	}

	updated, err := s.store.SetAvatar(ctx, user.ID, urls.Medium, false)
	if err != nil {
		s.log.WarnContext(ctx, "could not save the imported avatar", "user_id", user.ID, "error", err)
		return user
	}

	return updated
}
