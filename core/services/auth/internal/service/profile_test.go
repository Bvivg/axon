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

	updated, err := h.svc.UpdateProfile(context.Background(), registered.User.ID, uuid.Nil, domain.ProfilePatch{
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

	updated, err := h.svc.UpdateProfile(context.Background(), userID, uuid.Nil, domain.ProfilePatch{
		FirstName: ptr("Ada"),
		LastName:  ptr("Lovelace"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.DisplayName != "Ada Lovelace" {
		t.Errorf("DisplayName = %q, want \"Ada Lovelace\"", updated.DisplayName)
	}

	updated, err = h.svc.UpdateProfile(context.Background(), userID, uuid.Nil, domain.ProfilePatch{
		Nickname: ptr("countess"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.DisplayName != "countess" {
		t.Errorf("DisplayName = %q, want nickname to win over first/last", updated.DisplayName)
	}

	updated, err = h.svc.UpdateProfile(context.Background(), userID, uuid.Nil, domain.ProfilePatch{
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

	updated, err := h.svc.UpdateProfile(context.Background(), userID, uuid.Nil, domain.ProfilePatch{
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

	updated, err := h.svc.UpdateProfile(context.Background(), userID, uuid.Nil, domain.ProfilePatch{
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

	_, err := h.svc.UpdateProfile(context.Background(), registered.User.ID, uuid.Nil, domain.ProfilePatch{
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

	_, err := h.svc.UpdateProfile(context.Background(), registered.User.ID, uuid.Nil, domain.ProfilePatch{
		FirstName: ptr(string(long)),
	})
	var v *domain.ValidationError
	if !errors.As(err, &v) || v.Field != "first_name" {
		t.Fatalf("err = %v, want a validation error on first_name", err)
	}
}

func TestUpdateProfileUnknownUser(t *testing.T) {
	h := newHarness(t)

	_, err := h.svc.UpdateProfile(context.Background(), uuid.New(), uuid.Nil, domain.ProfilePatch{
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

func TestAnEmailChangeEndsEveryOtherSession(t *testing.T) {
	published := &recordingPublisher{}
	h := newHarness(t, withRevocation(published))

	registered := h.register(t, "ada@example.com", validPassword)
	current := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID

	var others []string
	for range 2 {
		res, err := h.svc.Login(context.Background(), service.LoginInput{Email: "ada@example.com", Password: validPassword})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		others = append(others, res.Tokens.RefreshToken)
	}

	if _, err := h.svc.UpdateProfile(onDevice(t), registered.User.ID, current, domain.ProfilePatch{Email: ptr("ada@newmail.com")}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	families := published.publishedFamilies()
	for _, presented := range others {
		family := h.storedToken(t, presented).FamilyID
		if _, err := h.svc.Refresh(context.Background(), presented, domain.Device{}); err == nil {
			t.Error("another session still refreshes after the email changed")
		}
		if !families[family] {
			t.Errorf("the revocation of family %s was not published", family)
		}
	}
	if families[current] {
		t.Error("the session that changed the email was revoked too")
	}
	if _, err := h.svc.Refresh(context.Background(), registered.Tokens.RefreshToken, domain.Device{}); err != nil {
		t.Errorf("the session that changed the email stopped working: %v", err)
	}

	var changed []domain.SecurityEvent
	for _, e := range h.store.recordedSecurityEvents() {
		if e.Action == domain.SecurityEmailChanged {
			changed = append(changed, e)
		}
	}
	if len(changed) != 1 {
		t.Fatalf("%d email_changed events, want 1", len(changed))
	}
	e := changed[0]
	if e.UserID != registered.User.ID || e.ActorID != registered.User.ID || e.Subject != current.String() ||
		e.EmailSHA256 != domain.EmailSHA256("ada@newmail.com") || e.Device != auditDevice {
		t.Errorf("event = %+v", e)
	}
}

func TestAProfileEditWithoutAnEmailChangeKeepsOtherSessions(t *testing.T) {
	published := &recordingPublisher{}
	h := newHarness(t, withRevocation(published))

	registered := h.register(t, "ada@example.com", validPassword)
	other, err := h.svc.Login(context.Background(), service.LoginInput{Email: "ada@example.com", Password: validPassword})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	current := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID
	if _, err := h.svc.UpdateProfile(context.Background(), registered.User.ID, current, domain.ProfilePatch{
		Email:     ptr("ADA@example.com"),
		FirstName: ptr("Ada"),
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	if _, err := h.svc.Refresh(context.Background(), other.Tokens.RefreshToken, domain.Device{}); err != nil {
		t.Errorf("a name change ended another session: %v", err)
	}
	if len(published.publishedFamilies()) != 0 {
		t.Error("a revocation was published without an email change")
	}
}

func TestAnEmailChangeToATakenAddressRevokesNothing(t *testing.T) {
	h := newHarness(t)

	registered := h.register(t, "ada@example.com", validPassword)
	h.register(t, "eve@example.com", validPassword)
	other, err := h.svc.Login(context.Background(), service.LoginInput{Email: "ada@example.com", Password: validPassword})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	current := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID
	_, err = h.svc.UpdateProfile(context.Background(), registered.User.ID, current, domain.ProfilePatch{Email: ptr("eve@example.com")})
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}
	if _, err := h.svc.Refresh(context.Background(), other.Tokens.RefreshToken, domain.Device{}); err != nil {
		t.Errorf("a refused email change ended another session: %v", err)
	}
}
