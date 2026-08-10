//go:build integration

package integration

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

// The reason this suite exists.
//
// Two refreshes racing for the same token is not a hypothetical: a client whose
// request times out and retries produces it, and so does a stolen token used
// while its owner is still active. Exactly one must win. If both did, one token
// would have handed out two live chains — which is the failure the whole
// rotation scheme exists to prevent.
//
// The guarantee lives in a WHERE clause (`used_at IS NULL`) inside a
// transaction. No unit test can observe it: with a mocked store there is no
// database to arbitrate, and over HTTP the timing is not controllable. Here it
// is a matter of pointing goroutines at one row.
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

	// And the database agrees: one spent token, one successor, nothing else.
	if got := countTokens(t, user.ID); got != 2 {
		t.Errorf("%d tokens exist for the user, want 2 (the spent one and its single successor)", got)
	}
}

// A rotation that loses the race must leave nothing behind. The successor is
// created in the same transaction as the mark, so a loser that still inserted
// its successor would mean a live token descended from a spent one.
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

// Revoking a family must reach every token in it and nothing outside it.
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

	// A user's other sign-ins are none of this family's business: revoking one
	// compromised chain must not sign them out of every device they own.
	if stored := mustLoad(t, repo, outside.TokenHash); stored.Revoked() {
		t.Error("an unrelated session was revoked with the compromised family")
	}
}

// Changing a password and signing every session out is one intent, so it has to
// be one transaction: a password change that leaves old refresh tokens alive has
// not locked anyone out, which is usually the whole reason for changing it.
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

// newToken builds a usable refresh token. The hash is a plain string because
// nothing here verifies one: these tests are about rows and transactions.
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
