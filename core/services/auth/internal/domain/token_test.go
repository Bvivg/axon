package domain_test

import (
	"testing"
	"time"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func TestRefreshTokenUsable(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	tests := []struct {
		name  string
		token domain.RefreshToken
		want  bool
	}{
		{
			name:  "fresh",
			token: domain.RefreshToken{ExpiresAt: future},
			want:  true,
		},
		{
			name:  "already exchanged",
			token: domain.RefreshToken{ExpiresAt: future, UsedAt: past},
		},
		{
			name:  "revoked",
			token: domain.RefreshToken{ExpiresAt: future, RevokedAt: past},
		},
		{
			name:  "expired",
			token: domain.RefreshToken{ExpiresAt: past},
		},
		{
			// A token is dead the instant it expires, not a moment after.
			name:  "expiring exactly now",
			token: domain.RefreshToken{ExpiresAt: now},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.Usable(now); got != tt.want {
				t.Errorf("Usable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRefreshTokenStatePredicates(t *testing.T) {
	now := time.Now()

	var zero domain.RefreshToken
	if zero.Used() {
		t.Error("a token with no UsedAt reported itself used")
	}
	if zero.Revoked() {
		t.Error("a token with no RevokedAt reported itself revoked")
	}

	used := domain.RefreshToken{UsedAt: now}
	if !used.Used() {
		t.Error("a token with UsedAt set did not report itself used")
	}

	revoked := domain.RefreshToken{RevokedAt: now}
	if !revoked.Revoked() {
		t.Error("a token with RevokedAt set did not report itself revoked")
	}
}
