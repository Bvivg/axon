package domain_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

var storedUsername = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		want   string
		reason string
	}{
		{name: "plain", in: "ada_lovelace", want: "ada_lovelace"},
		{name: "case and the at sign are dropped", in: "  @Ada_99 ", want: "ada_99"},
		{name: "shortest", in: "ada", want: "ada"},
		{name: "longest", in: strings.Repeat("a", 32), want: strings.Repeat("a", 32)},
		{name: "empty", in: " @ ", reason: "is required"},
		{name: "too short", in: "ad", reason: "is too short"},
		{name: "too long", in: strings.Repeat("a", 33), reason: "is too long"},
		{name: "starts with a digit", in: "9ada", reason: "must start with a letter"},
		{name: "starts with an underscore", in: "_ada", reason: "must start with a letter"},
		{name: "hyphen would clash with room ids", in: "ada-l", reason: "may only contain latin letters, digits and underscores"},
		{name: "non latin", in: "адa", reason: "may only contain latin letters, digits and underscores"},
		{name: "reserved", in: "Admin", reason: "is reserved"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ValidateUsername(tt.in)

			if tt.reason == "" {
				if err != nil {
					t.Fatalf("ValidateUsername(%q) error = %v", tt.in, err)
				}
				if got != tt.want {
					t.Errorf("ValidateUsername(%q) = %q, want %q", tt.in, got, tt.want)
				}
				return
			}

			v, ok := domain.AsValidationError(err)
			if !ok {
				t.Fatalf("ValidateUsername(%q) error = %v, want a validation error", tt.in, err)
			}
			if v.Field != "username" || v.Reason != tt.reason {
				t.Errorf("ValidateUsername(%q) = %s %s, want username %s", tt.in, v.Field, v.Reason, tt.reason)
			}
		})
	}
}

func TestUsernameStem(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		{email: "ada@example.com", want: "ada"},
		{email: "Ada.Lovelace+axon@example.com", want: "ada_lovelace_axon"},
		{email: "search-peer-1759300000000-12@axon.test", want: "search_peer_1759300000000_12"},
		{email: "42.ada__l.@example.com", want: "ada_l"},
		{email: "jo@example.com", want: "user"},
		{email: "1234@example.com", want: "user"},
		{email: "ёжик@example.com", want: "user"},
		{email: strings.Repeat("ab", 20) + "@example.com", want: strings.Repeat("ab", 16)},
		{email: strings.Repeat("a", 31) + ".b@example.com", want: strings.Repeat("a", 31)},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := domain.UsernameStem(tt.email)
			if got != tt.want {
				t.Errorf("UsernameStem(%q) = %q, want %q", tt.email, got, tt.want)
			}
			if !storedUsername.MatchString(got) {
				t.Errorf("UsernameStem(%q) = %q, which the database would refuse", tt.email, got)
			}
		})
	}
}

func TestUsernameWithSuffixFitsTheLimit(t *testing.T) {
	tests := []struct {
		stem string
		want string
	}{
		{stem: "ada", want: "ada_1a2b3c"},
		{stem: strings.Repeat("a", 32), want: strings.Repeat("a", 25) + "_1a2b3c"},
		{stem: strings.Repeat("a", 24) + "_b", want: strings.Repeat("a", 24) + "_1a2b3c"},
	}

	for _, tt := range tests {
		got := domain.UsernameWithSuffix(tt.stem, "1a2b3c")
		if got != tt.want {
			t.Errorf("UsernameWithSuffix(%q) = %q, want %q", tt.stem, got, tt.want)
		}
		if !storedUsername.MatchString(got) {
			t.Errorf("UsernameWithSuffix(%q) = %q, which the database would refuse", tt.stem, got)
		}
	}
}
