//go:build integration

package integration

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

func TestOnlyOneRotationWinsARace(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)
	now := time.Now().UTC()

	contested := newToken(user.ID, uuid.New(), "contested", now)
	if err := repo.CreateRefreshToken(ctx, contested); err != nil {
		t.Fatalf("seed the token: %v", err)
	}

	const racers = 8

	var (
		start   = make(chan struct{})
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		failure error
	)

	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			successor := newToken(user.ID, contested.FamilyID, "successor-"+uuid.NewString(), now)

			<-start
			won, err := repo.RotateRefreshToken(ctx, contested.ID, successor, time.Now().UTC())

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failure = err
				return
			}
			if won {
				wins++
			}
		}()
	}

	close(start)
	wg.Wait()

	if failure != nil {
		t.Fatalf("a rotation failed outright: %v", failure)
	}
	if wins != 1 {
		t.Fatalf("%d of %d concurrent rotations won; exactly one must", wins, racers)
	}

	if got := countTokens(t, user.ID); got != 2 {
		t.Errorf("%d tokens exist for the user, want 2 (the spent one and its single successor)", got)
	}
}

func TestALostRotationIssuesNothing(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)
	now := time.Now().UTC()

	spent := newToken(user.ID, uuid.New(), "spent", now)
	if err := repo.CreateRefreshToken(ctx, spent); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if won, err := repo.RotateRefreshToken(ctx, spent.ID, newToken(user.ID, spent.FamilyID, "first", now), now); err != nil || !won {
		t.Fatalf("the first rotation should have won: won=%v err=%v", won, err)
	}

	loser := newToken(user.ID, spent.FamilyID, "second", now)
	won, err := repo.RotateRefreshToken(ctx, spent.ID, loser, now)
	if err != nil {
		t.Fatalf("the second rotation errored instead of losing: %v", err)
	}
	if won {
		t.Fatal("a token was rotated twice")
	}

	if _, err := repo.RefreshTokenByHash(ctx, loser.TokenHash); err == nil {
		t.Error("the losing rotation still inserted its successor")
	}
}

func TestRevokeFamilyStopsAtTheFamilyBoundary(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)
	now := time.Now().UTC()

	compromised := uuid.New()
	other := uuid.New()

	inFamily := []domain.RefreshToken{
		newToken(user.ID, compromised, "compromised-1", now),
		newToken(user.ID, compromised, "compromised-2", now),
	}
	outside := newToken(user.ID, other, "other-session", now)

	for _, token := range append(inFamily, outside) {
		if err := repo.CreateRefreshToken(ctx, token); err != nil {
			t.Fatalf("seed %s: %v", token.TokenHash, err)
		}
	}

	revoked, err := repo.RevokeFamily(ctx, compromised, now)
	if err != nil {
		t.Fatalf("revoke family: %v", err)
	}
	if revoked != int64(len(inFamily)) {
		t.Errorf("revoked %d tokens, want %d", revoked, len(inFamily))
	}

	for _, token := range inFamily {
		if stored := mustLoad(t, repo, token.TokenHash); !stored.Revoked() {
			t.Errorf("%s survived the family revocation", token.TokenHash)
		}
	}

	if stored := mustLoad(t, repo, outside.TokenHash); stored.Revoked() {
		t.Error("an unrelated session was revoked with the compromised family")
	}
}

func TestSetPasswordAndRevokeSessionsIsAtomic(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)
	now := time.Now().UTC()

	for i := range 3 {
		token := newToken(user.ID, uuid.New(), "session-"+string(rune('a'+i)), now)
		if err := repo.CreateRefreshToken(ctx, token); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	const newHash = "$argon2id$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$bmV3aGFzaA"
	if err := repo.SetPasswordAndRevokeSessions(ctx, user.ID, newHash, now); err != nil {
		t.Fatalf("set password: %v", err)
	}

	cred, err := repo.CredentialByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}
	if cred.PasswordHash != newHash {
		t.Error("the password was not changed")
	}

	if live := countLiveTokens(t, user.ID, now); live != 0 {
		t.Errorf("%d sessions survived a password change", live)
	}
}

func TestAnEmailChangeRevokesEveryOtherFamilyInOneTransaction(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)
	bystander := createUser(t, repo)
	now := time.Now().UTC()

	current, first, second := uuid.New(), uuid.New(), uuid.New()
	for _, token := range []domain.RefreshToken{
		newToken(user.ID, current, "current-1", now),
		newToken(user.ID, first, "first-1", now),
		newToken(user.ID, first, "first-2", now),
		newToken(user.ID, second, "second-1", now),
		newToken(bystander.ID, uuid.New(), "bystander-1", now),
	} {
		if err := repo.CreateRefreshToken(ctx, token); err != nil {
			t.Fatalf("seed %s: %v", token.TokenHash, err)
		}
	}

	update := domain.ProfileUpdate{Email: "changed-" + uuid.NewString() + "@axon.test", Username: user.Username}
	updated, revoked, err := repo.UpdateProfileRevokingOtherSessions(ctx, user.ID, update, current, now)
	if err != nil {
		t.Fatalf("UpdateProfileRevokingOtherSessions: %v", err)
	}
	if updated.Email != update.Email || updated.EmailVerified {
		t.Errorf("updated = %s verified=%v, want %s unverified", updated.Email, updated.EmailVerified, update.Email)
	}

	got := map[uuid.UUID]bool{}
	for _, family := range revoked {
		if got[family] {
			t.Errorf("family %s reported twice", family)
		}
		got[family] = true
	}
	if len(got) != 2 || !got[first] || !got[second] {
		t.Errorf("revoked families = %v, want exactly %s and %s", revoked, first, second)
	}

	for hash, want := range map[string]bool{"current-1": false, "first-1": true, "first-2": true, "second-1": true, "bystander-1": false} {
		if stored := mustLoad(t, repo, hash); stored.Revoked() != want {
			t.Errorf("%s revoked = %v, want %v", hash, stored.Revoked(), want)
		}
	}
}

func TestARefusedEmailChangeRevokesNothing(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)
	other := createUser(t, repo)
	now := time.Now().UTC()

	if err := repo.CreateRefreshToken(ctx, newToken(user.ID, uuid.New(), "other-session", now)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, _, err := repo.UpdateProfileRevokingOtherSessions(ctx, user.ID,
		domain.ProfileUpdate{Email: other.Email, Username: user.Username}, uuid.New(), now)
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}
	if stored := mustLoad(t, repo, "other-session"); stored.Revoked() {
		t.Error("a refused email change still revoked a session")
	}
}

func newToken(userID, familyID uuid.UUID, hash string, now time.Time) domain.RefreshToken {
	return domain.RefreshToken{
		ID:        uuid.New(),
		UserID:    userID,
		FamilyID:  familyID,
		TokenHash: hash,
		IssuedAt:  now,
		ExpiresAt: now.Add(720 * time.Hour),
	}
}

func mustLoad(t *testing.T, repo *repository.Repository, hash string) domain.RefreshToken {
	t.Helper()

	token, err := repo.RefreshTokenByHash(t.Context(), hash)
	if err != nil {
		t.Fatalf("load %s: %v", hash, err)
	}
	return token
}

func countTokens(t *testing.T, userID uuid.UUID) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM refresh_tokens WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	return n
}

func countLiveTokens(t *testing.T, userID uuid.UUID, now time.Time) int {
	t.Helper()

	var n int
	err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM refresh_tokens
		WHERE user_id = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > $2`,
		userID, now).Scan(&n)
	if err != nil {
		t.Fatalf("count live tokens: %v", err)
	}
	return n
}
