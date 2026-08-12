package service_test

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

type fakeStore struct {
	mu sync.Mutex

	users        map[uuid.UUID]domain.User
	usersByMail  map[string]uuid.UUID
	credentials  map[uuid.UUID]domain.Credential
	tokens       map[uuid.UUID]domain.RefreshToken
	tokensByHash map[string]uuid.UUID
	oauth        map[string]domain.OauthAccount

	failOn map[string]error

	beforeRotate func()

	beforeLink func()
}

var _ service.Store = (*fakeStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:        make(map[uuid.UUID]domain.User),
		usersByMail:  make(map[string]uuid.UUID),
		credentials:  make(map[uuid.UUID]domain.Credential),
		tokens:       make(map[uuid.UUID]domain.RefreshToken),
		tokensByHash: make(map[string]uuid.UUID),
		oauth:        make(map[string]domain.OauthAccount),
		failOn:       make(map[string]error),
	}
}

func (s *fakeStore) fail(method string) error {
	if err, ok := s.failOn[method]; ok {
		return err
	}
	return nil
}

func (s *fakeStore) CreateUserWithPassword(_ context.Context, u domain.User, passwordHash string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("CreateUserWithPassword"); err != nil {
		return domain.User{}, err
	}
	if _, taken := s.usersByMail[u.Email]; taken {
		return domain.User{}, domain.ErrEmailTaken
	}

	now := time.Now().UTC()
	u.CreatedAt, u.UpdatedAt = now, now

	s.users[u.ID] = u
	s.usersByMail[u.Email] = u.ID
	s.credentials[u.ID] = domain.Credential{UserID: u.ID, PasswordHash: passwordHash, UpdatedAt: now}

	return u, nil
}

func (s *fakeStore) CreateUserWithOauthAccount(_ context.Context, u domain.User, a domain.OauthAccount) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("CreateUserWithOauthAccount"); err != nil {
		return domain.User{}, err
	}
	if _, taken := s.usersByMail[u.Email]; taken {
		return domain.User{}, domain.ErrEmailTaken
	}

	now := time.Now().UTC()
	u.CreatedAt, u.UpdatedAt = now, now

	s.users[u.ID] = u
	s.usersByMail[u.Email] = u.ID

	a.UserID = u.ID
	a.LinkedAt = now
	s.oauth[oauthKey(a.Provider, a.ProviderUserID)] = a

	return u, nil
}

func (s *fakeStore) UserByEmail(_ context.Context, email string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("UserByEmail"); err != nil {
		return domain.User{}, err
	}

	id, ok := s.usersByMail[email]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	return s.users[id], nil
}

func (s *fakeStore) UserByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("UserByID"); err != nil {
		return domain.User{}, err
	}

	u, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	return u, nil
}

func (s *fakeStore) UpdateUserProfile(_ context.Context, id uuid.UUID, displayName, avatarURL string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("UpdateUserProfile"); err != nil {
		return domain.User{}, err
	}

	u, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}

	if u.DisplayName == "" {
		u.DisplayName = displayName
	}
	if u.AvatarURL == "" {
		u.AvatarURL = avatarURL
	}
	u.UpdatedAt = time.Now().UTC()
	s.users[id] = u

	return u, nil
}

func (s *fakeStore) SetCredential(_ context.Context, userID uuid.UUID, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("SetCredential"); err != nil {
		return err
	}

	s.credentials[userID] = domain.Credential{
		UserID: userID, PasswordHash: passwordHash, UpdatedAt: time.Now().UTC(),
	}
	return nil
}

func (s *fakeStore) CredentialByUserID(_ context.Context, userID uuid.UUID) (domain.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("CredentialByUserID"); err != nil {
		return domain.Credential{}, err
	}

	c, ok := s.credentials[userID]
	if !ok {
		return domain.Credential{}, domain.ErrNoPassword
	}
	return c, nil
}

func (s *fakeStore) CreateRefreshToken(_ context.Context, t domain.RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("CreateRefreshToken"); err != nil {
		return err
	}
	return s.createRefreshTokenLocked(t)
}

func (s *fakeStore) createRefreshTokenLocked(t domain.RefreshToken) error {
	if t.IssuedAt.IsZero() {
		t.IssuedAt = time.Now().UTC()
	}
	s.tokens[t.ID] = t
	s.tokensByHash[t.TokenHash] = t.ID
	return nil
}

func (s *fakeStore) RefreshTokenByHash(_ context.Context, hash string) (domain.RefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("RefreshTokenByHash"); err != nil {
		return domain.RefreshToken{}, err
	}

	id, ok := s.tokensByHash[hash]
	if !ok {
		return domain.RefreshToken{}, domain.ErrRefreshTokenInvalid
	}

	return s.tokens[id], nil
}

func (s *fakeStore) RotateRefreshToken(_ context.Context, spentID uuid.UUID, next domain.RefreshToken, at time.Time) (bool, error) {
	if s.beforeRotate != nil {
		s.beforeRotate()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("RotateRefreshToken"); err != nil {
		return false, err
	}

	spent, ok := s.tokens[spentID]
	if !ok {
		return false, nil
	}

	if spent.Used() || spent.Revoked() {
		return false, nil
	}

	spent.UsedAt = at
	s.tokens[spentID] = spent

	return true, s.createRefreshTokenLocked(next)
}

func (s *fakeStore) RevokeFamily(_ context.Context, familyID uuid.UUID, at time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("RevokeFamily"); err != nil {
		return 0, err
	}

	var n int64
	for id, t := range s.tokens {
		if t.FamilyID == familyID && !t.Revoked() {
			t.RevokedAt = at
			s.tokens[id] = t
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) RevokeAllForUser(_ context.Context, userID uuid.UUID, at time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("RevokeAllForUser"); err != nil {
		return 0, err
	}

	var n int64
	for id, t := range s.tokens {
		if t.UserID == userID && !t.Revoked() {
			t.RevokedAt = at
			s.tokens[id] = t
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) OauthAccountByProviderID(_ context.Context, p domain.Provider, providerUserID string) (domain.OauthAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("OauthAccountByProviderID"); err != nil {
		return domain.OauthAccount{}, err
	}

	a, ok := s.oauth[oauthKey(p, providerUserID)]
	if !ok {
		return domain.OauthAccount{}, domain.ErrUserNotFound
	}
	return a, nil
}

func (s *fakeStore) LinkOauthAccount(_ context.Context, a domain.OauthAccount) error {
	if s.beforeLink != nil {
		s.beforeLink()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("LinkOauthAccount"); err != nil {
		return err
	}

	key := oauthKey(a.Provider, a.ProviderUserID)

	if existing, ok := s.oauth[key]; ok && existing.UserID != a.UserID {
		return domain.ErrOauthIdentityClaimed
	}
	if a.LinkedAt.IsZero() {
		a.LinkedAt = time.Now().UTC()
	}
	s.oauth[key] = a
	return nil
}

func (s *fakeStore) linkDirectly(a domain.OauthAccount) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a.LinkedAt.IsZero() {
		a.LinkedAt = time.Now().UTC()
	}
	s.oauth[oauthKey(a.Provider, a.ProviderUserID)] = a
}

func oauthKey(p domain.Provider, providerUserID string) string {
	return p.String() + "\x00" + providerUserID
}

func (s *fakeStore) tokenByHash(hash string) (domain.RefreshToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, ok := s.tokensByHash[hash]
	if !ok {
		return domain.RefreshToken{}, false
	}
	return s.tokens[id], true
}

func (s *fakeStore) liveTokensForUser(userID uuid.UUID, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	var n int
	for _, t := range s.tokens {
		if t.UserID == userID && t.Usable(now) {
			n++
		}
	}
	return n
}
