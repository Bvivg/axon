package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

func ptr(s string) *string { return &s }

func TestUpdateProfileAppliesOnlyPresentFields(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	updated, err := h.svc.UpdateProfile(context.Background(), registered.User.ID, domain.ProfilePatch{
		FirstName: ptr("Ada"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	if updated.FirstName != "Ada" {
		t.Errorf("FirstName = %q, want Ada", updated.FirstName)
	}
	if updated.Email != "ada@example.com" {
		t.Errorf("Email = %q, want unchanged", updated.Email)
	}
	if updated.LastName != "" {
		t.Errorf("LastName = %q, want empty (never set)", updated.LastName)
	}
}

func TestUpdateProfileDisplayNamePriority(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)
	userID := registered.User.ID

	updated, err := h.svc.UpdateProfile(context.Background(), userID, domain.ProfilePatch{
		FirstName: ptr("Ada"),
		LastName:  ptr("Lovelace"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.DisplayName != "Ada Lovelace" {
		t.Errorf("DisplayName = %q, want \"Ada Lovelace\"", updated.DisplayName)
	}

	updated, err = h.svc.UpdateProfile(context.Background(), userID, domain.ProfilePatch{
		Nickname: ptr("countess"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.DisplayName != "countess" {
		t.Errorf("DisplayName = %q, want nickname to win over first/last", updated.DisplayName)
	}

	updated, err = h.svc.UpdateProfile(context.Background(), userID, domain.ProfilePatch{
		Nickname: ptr(""),
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.DisplayName != "Ada Lovelace" {
		t.Errorf("DisplayName = %q, want fallback to first/last once nickname is cleared", updated.DisplayName)
	}
}

func TestUpdateProfileEmailChangeResetsVerification(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)
	userID := registered.User.ID

	h.store.mu.Lock()
	u := h.store.users[userID]
	u.EmailVerified = true
	h.store.users[userID] = u
	h.store.mu.Unlock()

	updated, err := h.svc.UpdateProfile(context.Background(), userID, domain.ProfilePatch{
		Email: ptr("ada@newmail.com"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.Email != "ada@newmail.com" {
		t.Errorf("Email = %q, want ada@newmail.com", updated.Email)
	}
	if updated.EmailVerified {
		t.Error("EmailVerified stayed true across an email change")
	}
}

func TestUpdateProfileSameEmailKeepsVerification(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)
	userID := registered.User.ID

	h.store.mu.Lock()
	u := h.store.users[userID]
	u.EmailVerified = true
	h.store.users[userID] = u
	h.store.mu.Unlock()

	updated, err := h.svc.UpdateProfile(context.Background(), userID, domain.ProfilePatch{
		Email: ptr("ADA@EXAMPLE.COM"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if !updated.EmailVerified {
		t.Error("EmailVerified was reset even though the normalized email did not change")
	}
}

func TestUpdateProfileRejectsInvalidEmail(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	_, err := h.svc.UpdateProfile(context.Background(), registered.User.ID, domain.ProfilePatch{
		Email: ptr("not-an-email"),
	})
	var v *domain.ValidationError
	if !errors.As(err, &v) || v.Field != "email" {
		t.Fatalf("err = %v, want a validation error on email", err)
	}
}

func TestUpdateProfileRejectsOverlongName(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	long := make([]byte, domain.MaxDisplayNameLength+1)
	for i := range long {
		long[i] = 'a'
	}

	_, err := h.svc.UpdateProfile(context.Background(), registered.User.ID, domain.ProfilePatch{
		FirstName: ptr(string(long)),
	})
	var v *domain.ValidationError
	if !errors.As(err, &v) || v.Field != "first_name" {
		t.Fatalf("err = %v, want a validation error on first_name", err)
	}
}

func TestUpdateProfileUnknownUser(t *testing.T) {
	h := newHarness(t)

	_, err := h.svc.UpdateProfile(context.Background(), uuid.New(), domain.ProfilePatch{
		FirstName: ptr("Ada"),
	})
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

func TestUploadAvatarWithoutStorageConfigured(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	_, err := h.svc.UploadAvatar(context.Background(), registered.User.ID, []byte("not an image"))
	if !errors.Is(err, service.ErrAvatarStorageUnavailable) {
		t.Fatalf("err = %v, want ErrAvatarStorageUnavailable", err)
	}
}
