//go:build integration

package integration

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const (
	usernameTestHash       = "$argon2id$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$aGFzaA"
	versionBeforeUsernames = 4
	versionWithUsernames   = 5
	migrationLockTimeout   = time.Minute
)

func migrateTo(t *testing.T, version uint) {
	t.Helper()

	m, err := migrate.New("file://../migrations", "pgx5://"+authDSN[len("postgres://"):])
	if err != nil {
		t.Fatalf("open migrations: %v", err)
	}
	defer func() { _, _ = m.Close() }()

	m.LockTimeout = migrationLockTimeout
	if err := m.Migrate(version); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate to %d: %v", version, err)
	}
}

func TestTheUsernameMigrationNamesExistingAccounts(t *testing.T) {
	newRepo(t)
	ctx := t.Context()

	migrateTo(t, versionBeforeUsernames)
	t.Cleanup(func() { migrateTo(t, versionWithUsernames) })

	type naming int
	const (
		natural naming = iota
		suffixed
		fallback
	)
	type legacy struct {
		id     uuid.UUID
		email  string
		naming naming
	}
	crowded := uuid.New()
	accounts := []legacy{
		{email: "ada.lovelace@first.test"},
		{email: "ada.lovelace@second.test", naming: suffixed},
		{email: "42.bob@axon.test"},
		{email: "x@axon.test"},
		{email: "yz@axon.test", naming: suffixed},
		{email: "a.very.long.local.part.that.never.ends.here@axon.test"},
		{email: "admin@axon.test", naming: suffixed},
		{id: crowded, email: "ab@axon.test", naming: fallback},
		{email: "user." + idHex(crowded)[:6] + "@axon.test"},
	}

	created := time.Now().UTC().Add(-time.Hour)
	for i := range accounts {
		if accounts[i].id == uuid.Nil {
			accounts[i].id = uuid.New()
		}
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, created_at) VALUES ($1, $2, $3)`,
			accounts[i].id, accounts[i].email, created.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("insert %s: %v", accounts[i].email, err)
		}
	}

	migrateTo(t, versionWithUsernames)

	for _, a := range accounts {
		var got string
		if err := pool.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, a.id).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", a.email, err)
		}

		want := domain.UsernameStem(a.email)
		switch a.naming {
		case suffixed:
			want = domain.UsernameWithSuffix(want, idHex(a.id)[:6])
		case fallback:
			want = "u" + idHex(a.id)[:31]
		}
		if got != want {
			t.Errorf("%s got %q, want %q", a.email, got, want)
		}
	}
}

func idHex(id uuid.UUID) string {
	return strings.ReplaceAll(id.String(), "-", "")
}

func TestRegistrationRollsBackOnATakenUsername(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	first := createUser(t, repo)
	second := "user-" + uuid.NewString() + "@axon.test"

	_, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:       uuid.New(),
		Username: first.Username,
		Email:    second,
	}, usernameTestHash)
	if !errors.Is(err, domain.ErrUsernameTaken) {
		t.Fatalf("err = %v, want ErrUsernameTaken", err)
	}
	if n := countUsers(t, second); n != 0 {
		t.Errorf("%d users hold %q after the refused registration, want 0", n, second)
	}
}

func TestUpdateProfileToATakenUsernameIsRefused(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	ada := createUser(t, repo)
	eve := createUser(t, repo)

	_, err := repo.UpdateProfile(ctx, eve.ID, domain.ProfileUpdate{
		Email:    eve.Email,
		Username: ada.Username,
	})
	if !errors.Is(err, domain.ErrUsernameTaken) {
		t.Fatalf("err = %v, want ErrUsernameTaken", err)
	}
}

func TestUserByUsernameAndExactSearch(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	caller := createUser(t, repo)
	target := createUser(t, repo)

	found, err := repo.UserByUsername(ctx, target.Username)
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if found.ID != target.ID || found.Username != target.Username {
		t.Errorf("found %s (%q), want %s", found.ID, found.Username, target.ID)
	}

	if _, err := repo.UserByUsername(ctx, newUsername()); !errors.Is(err, domain.ErrUserNotFound) {
		t.Errorf("unknown username err = %v, want ErrUserNotFound", err)
	}

	users, err := repo.SearchUsers(ctx, "@"+strings.ToUpper(target.Username), caller.ID, 20, nil)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) != 1 || users[0].ID != target.ID {
		t.Errorf("SearchUsers = %v, want exactly %s", users, target.ID)
	}
}

func TestConcurrentRegistrationsWithOneUsernameProduceOneAccount(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	username := newUsername()

	const racers = 4

	var (
		start   = make(chan struct{})
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		taken   int
	)

	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			<-start
			_, err := repo.CreateUserWithPassword(ctx, domain.User{
				ID:       uuid.New(),
				Username: username,
				Email:    "racer-" + uuid.NewString() + "@axon.test",
			}, usernameTestHash)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created++
			case errors.Is(err, domain.ErrUsernameTaken):
				taken++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}

	close(start)
	wg.Wait()

	if created != 1 || taken != racers-1 {
		t.Errorf("created %d, refused as taken %d; want 1 and %d", created, taken, racers-1)
	}
}
