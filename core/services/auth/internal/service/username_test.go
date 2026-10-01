package service_test

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

func registerAs(h *harness, email string, username *string) (service.Result, error) {
	return h.svc.Register(context.Background(), service.RegisterInput{
		Email:    email,
		Password: validPassword,
		Username: username,
	})
}

func TestRegisterKeepsTheChosenUsername(t *testing.T) {
	h := newHarness(t)

	res, err := registerAs(h, "ada@example.com", ptr("  @Ada_L "))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if res.User.Username != "ada_l" {
		t.Errorf("username = %q, want ada_l", res.User.Username)
	}
}

func TestRegisterRejectsATakenUsername(t *testing.T) {
	h := newHarness(t)
	if _, err := registerAs(h, "ada@example.com", ptr("ada")); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	_, err := registerAs(h, "eve@example.com", ptr("ADA"))
	if !errors.Is(err, domain.ErrUsernameTaken) {
		t.Fatalf("err = %v, want ErrUsernameTaken", err)
	}
	if _, err := h.store.UserByEmail(context.Background(), "eve@example.com"); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("the refused registration left an account behind: %v", err)
	}
}

func TestRegisterRejectsAnUnusableUsername(t *testing.T) {
	for _, name := range []string{"", "1ada", "ad", "ada-l", "admin"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)

			_, err := registerAs(h, "ada@example.com", ptr(name))
			if v, ok := domain.AsValidationError(err); !ok || v.Field != "username" {
				t.Fatalf("err = %v, want a username validation error", err)
			}
		})
	}
}

func TestRegisterWithoutAUsernameGeneratesOneFromTheEmail(t *testing.T) {
	h := newHarness(t)

	res := h.register(t, "Ada.Lovelace@example.com", validPassword)
	if res.User.Username != "ada_lovelace" {
		t.Errorf("username = %q, want ada_lovelace", res.User.Username)
	}
}

func TestAGeneratedUsernameStepsAroundATakenOrReservedName(t *testing.T) {
	for name, tc := range map[string]struct {
		taken string
		email string
		want  string
	}{
		"taken":    {taken: "ada@example.com", email: "ada@other.test", want: `^ada_[0-9a-f]{6}$`},
		"reserved": {email: "admin@example.com", want: `^admin_[0-9a-f]{6}$`},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if tc.taken != "" {
				h.register(t, tc.taken, validPassword)
			}

			res := h.register(t, tc.email, validPassword)
			if !regexp.MustCompile(tc.want).MatchString(res.User.Username) {
				t.Errorf("username = %q, want %s", res.User.Username, tc.want)
			}
		})
	}
}

func TestUsernameGenerationGivesUpAfterRepeatedCollisions(t *testing.T) {
	h := newHarness(t)
	h.store.failOn["CreateUserWithPassword"] = domain.ErrUsernameTaken

	_, err := registerAs(h, "ada@example.com", nil)
	if !errors.Is(err, domain.ErrUsernameTaken) {
		t.Fatalf("err = %v, want ErrUsernameTaken", err)
	}
}

func TestOauthSignInGeneratesAUsername(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	first := h.signIn(t, identity("subject-1", "grace.hopper@example.test"))
	if first.User.Username != "grace_hopper" {
		t.Errorf("username = %q, want grace_hopper", first.User.Username)
	}

	second := h.signIn(t, identity("subject-2", "grace.hopper@other.test"))
	if !regexp.MustCompile(`^grace_hopper_[0-9a-f]{6}$`).MatchString(second.User.Username) {
		t.Errorf("username = %q, want a suffixed grace_hopper", second.User.Username)
	}
}

func TestUpdateProfileChangesTheUsername(t *testing.T) {
	h := newHarness(t)
	ada := h.register(t, "ada@example.com", validPassword)
	eve := h.register(t, "eve@example.com", validPassword)

	updated, err := h.svc.UpdateProfile(context.Background(), ada.User.ID, domain.ProfilePatch{Username: ptr("@Countess")})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.Username != "countess" {
		t.Errorf("username = %q, want countess", updated.Username)
	}

	_, err = h.svc.UpdateProfile(context.Background(), eve.User.ID, domain.ProfilePatch{Username: ptr("countess")})
	if !errors.Is(err, domain.ErrUsernameTaken) {
		t.Errorf("err = %v, want ErrUsernameTaken", err)
	}
}

func TestUpdateProfileCannotClearTheUsername(t *testing.T) {
	h := newHarness(t)
	ada := h.register(t, "ada@example.com", validPassword)

	_, err := h.svc.UpdateProfile(context.Background(), ada.User.ID, domain.ProfilePatch{Username: ptr(" ")})
	if v, ok := domain.AsValidationError(err); !ok || v.Field != "username" {
		t.Fatalf("err = %v, want a username validation error", err)
	}
}

func TestUpdateProfileKeepsTheCurrentUsernameEvenIfReserved(t *testing.T) {
	h := newHarness(t)
	ada := h.register(t, "ada@example.com", validPassword)
	h.store.users[ada.User.ID] = withUsername(h.store.users[ada.User.ID], "admin")

	updated, err := h.svc.UpdateProfile(context.Background(), ada.User.ID, domain.ProfilePatch{
		Username: ptr("Admin"),
		Nickname: ptr("Ada"),
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.Username != "admin" || updated.Nickname != "Ada" {
		t.Errorf("username = %q, nickname = %q", updated.Username, updated.Nickname)
	}
}

func TestSearchUsersFindsAUsername(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	target := h.register(t, "eve.online@example.com", validPassword)

	for _, query := range []string{"eve_online", "@Eve_Online"} {
		found, err := h.svc.SearchUsers(context.Background(), caller.User.ID, query)
		if err != nil {
			t.Fatalf("SearchUsers(%q): %v", query, err)
		}
		if len(found) != 1 || found[0].User.ID != target.User.ID {
			t.Errorf("SearchUsers(%q) = %v, want exactly %s", query, found, target.User.ID)
		}
	}
}

func TestPublicProfileByUsername(t *testing.T) {
	h := newHarness(t)
	eve := h.register(t, "eve@example.com", validPassword)

	profile, err := h.svc.PublicProfileByUsername(context.Background(), " @EVE")
	if err != nil {
		t.Fatalf("PublicProfileByUsername: %v", err)
	}
	if profile.User.ID != eve.User.ID {
		t.Errorf("profile = %s, want %s", profile.User.ID, eve.User.ID)
	}

	for _, unknown := range []string{"nobody", "", "@"} {
		if _, err := h.svc.PublicProfileByUsername(context.Background(), unknown); !errors.Is(err, domain.ErrUserNotFound) {
			t.Errorf("PublicProfileByUsername(%q) err = %v, want ErrUserNotFound", unknown, err)
		}
	}
}

func withUsername(u domain.User, username string) domain.User {
	u.Username = username
	return u
}
