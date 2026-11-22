package service_test

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"

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

	roles           map[uuid.UUID]domain.Role
	rolePermissions map[uuid.UUID]map[string]bool
	userRoles       map[uuid.UUID]map[uuid.UUID]bool

	securityEvents []domain.SecurityEvent

	adminActions []authz.Action

	adminRoles   map[uuid.UUID]domain.AdminRole
	auditExport  []domain.AuditEvent
	profileLocks map[uuid.UUID]int64

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

		roles: map[uuid.UUID]domain.Role{
			domain.RoleUser:      {ID: domain.RoleUser, Name: "user"},
			domain.RoleAdmin:     {ID: domain.RoleAdmin, Name: "admin"},
			domain.RoleModerator: {ID: domain.RoleModerator, Name: "moderator"},
		},
		rolePermissions: map[uuid.UUID]map[string]bool{
			domain.RoleUser: {
				"user:manage": true, "session:manage": true, "chat:manage": true,
				"game:manage": true, "calling:manage": true,
			},
			domain.RoleAdmin: {
				"user:manage": true, "session:manage": true, "chat:manage": true,
				"game:manage": true, "calling:manage": true, "rbac:manage": true,
				"admin.users:manage": true, "admin.games:manage": true, "admin.chat:manage": true, "admin.audit:manage": true,
			},
			domain.RoleModerator: {
				"admin.users:read": true, "admin.games:manage": true, "admin.chat:manage": true, "admin.audit:read": true, "rbac:read": true,
			},
		},
		userRoles: make(map[uuid.UUID]map[uuid.UUID]bool),

		failOn: make(map[string]error),
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
	if s.usernameTakenLocked(u.Username, u.ID) {
		return domain.User{}, domain.ErrUsernameTaken
	}

	now := time.Now().UTC()
	u.CreatedAt, u.UpdatedAt = now, now

	s.users[u.ID] = u
	s.usersByMail[u.Email] = u.ID
	s.credentials[u.ID] = domain.Credential{UserID: u.ID, PasswordHash: passwordHash, UpdatedAt: now}
	s.userRoles[u.ID] = map[uuid.UUID]bool{domain.RoleUser: true}

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
	if s.usernameTakenLocked(u.Username, u.ID) {
		return domain.User{}, domain.ErrUsernameTaken
	}

	now := time.Now().UTC()
	u.CreatedAt, u.UpdatedAt = now, now

	s.users[u.ID] = u
	s.usersByMail[u.Email] = u.ID
	s.userRoles[u.ID] = map[uuid.UUID]bool{domain.RoleUser: true}

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

func (s *fakeStore) UserByUsername(_ context.Context, username string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("UserByUsername"); err != nil {
		return domain.User{}, err
	}

	for _, u := range s.users {
		if u.Username == username {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrUserNotFound
}

func (s *fakeStore) usernameTakenLocked(username string, owner uuid.UUID) bool {
	for id, u := range s.users {
		if id != owner && u.Username == username {
			return true
		}
	}
	return false
}

func (s *fakeStore) UpdateProfile(_ context.Context, id uuid.UUID, in domain.ProfileUpdate) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("UpdateProfile"); err != nil {
		return domain.User{}, err
	}

	u, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}

	if s.usernameTakenLocked(in.Username, id) {
		return domain.User{}, domain.ErrUsernameTaken
	}
	if owner, taken := s.usersByMail[in.Email]; taken && owner != id {
		return domain.User{}, domain.ErrEmailTaken
	}

	delete(s.usersByMail, u.Email)
	s.usersByMail[in.Email] = id
	u.Email = in.Email
	u.EmailVerified = in.EmailVerified
	u.FirstName = in.FirstName
	u.LastName = in.LastName
	u.Nickname = in.Nickname
	u.Username = in.Username
	u.DisplayName = in.DisplayName
	u.UpdatedAt = time.Now().UTC()
	s.users[id] = u

	return u, nil
}

func (s *fakeStore) UpdateProfileRevokingOtherSessions(
	ctx context.Context, id uuid.UUID, in domain.ProfileUpdate, keepFamilyID uuid.UUID, at time.Time,
) (domain.User, []uuid.UUID, error) {
	if err := s.fail("UpdateProfileRevokingOtherSessions"); err != nil {
		return domain.User{}, nil, err
	}

	updated, err := s.UpdateProfile(ctx, id, in)
	if err != nil {
		return domain.User{}, nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[uuid.UUID]bool)
	var families []uuid.UUID
	for tokenID, t := range s.tokens {
		if t.UserID == id && t.FamilyID != keepFamilyID && !t.Revoked() {
			t.RevokedAt = at
			s.tokens[tokenID] = t
			if !seen[t.FamilyID] {
				seen[t.FamilyID] = true
				families = append(families, t.FamilyID)
			}
		}
	}
	return updated, families, nil
}

func (s *fakeStore) UpdateLastSeen(_ context.Context, id uuid.UUID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("UpdateLastSeen"); err != nil {
		return err
	}

	u, ok := s.users[id]
	if !ok {
		return domain.ErrUserNotFound
	}

	u.LastSeenAt = &at
	s.users[id] = u

	return nil
}

func (s *fakeStore) SetAvatar(_ context.Context, id uuid.UUID, avatarURL string, custom bool) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("SetAvatar"); err != nil {
		return domain.User{}, err
	}

	u, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}

	if custom || !u.AvatarIsCustom {
		u.AvatarURL = avatarURL
		u.AvatarIsCustom = custom
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

func (s *fakeStore) RevokeFamilyForUser(_ context.Context, userID, familyID uuid.UUID, at time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("RevokeFamilyForUser"); err != nil {
		return 0, err
	}

	var n int64
	for id, t := range s.tokens {
		if t.UserID == userID && t.FamilyID == familyID && !t.Revoked() {
			t.RevokedAt = at
			s.tokens[id] = t
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) Sessions(_ context.Context, userID uuid.UUID, at time.Time) ([]domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("Sessions"); err != nil {
		return nil, err
	}

	type acc struct {
		startedAt             time.Time
		lastUsedAt            time.Time
		lastUserAgent, lastIP string
	}
	byFamily := make(map[uuid.UUID]acc)

	for _, t := range s.tokens {
		if t.UserID != userID || t.Revoked() || !t.ExpiresAt.After(at) {
			continue
		}
		a, ok := byFamily[t.FamilyID]
		if !ok {
			a.startedAt = t.IssuedAt
			a.lastUsedAt = t.IssuedAt
			a.lastUserAgent, a.lastIP = t.UserAgent, t.IP
		} else {
			if t.IssuedAt.Before(a.startedAt) {
				a.startedAt = t.IssuedAt
			}
			if !t.IssuedAt.Before(a.lastUsedAt) {
				a.lastUsedAt = t.IssuedAt
				a.lastUserAgent, a.lastIP = t.UserAgent, t.IP
			}
		}
		byFamily[t.FamilyID] = a
	}

	out := make([]domain.Session, 0, len(byFamily))
	for familyID, a := range byFamily {
		out = append(out, domain.Session{
			FamilyID:   familyID,
			UserAgent:  a.lastUserAgent,
			IP:         a.lastIP,
			StartedAt:  a.startedAt,
			LastUsedAt: a.lastUsedAt,
		})
	}
	return out, nil
}

func (s *fakeStore) ActiveFamiliesForUsers(
	_ context.Context,
	userIDs []uuid.UUID,
	at time.Time,
) (map[uuid.UUID][]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("ActiveFamiliesForUsers"); err != nil {
		return nil, err
	}

	want := make(map[uuid.UUID]bool, len(userIDs))
	for _, id := range userIDs {
		want[id] = true
	}

	seen := make(map[[2]uuid.UUID]bool)
	families := make(map[uuid.UUID][]uuid.UUID)
	for _, t := range s.tokens {
		if !want[t.UserID] || t.Revoked() || !t.ExpiresAt.After(at) {
			continue
		}
		key := [2]uuid.UUID{t.UserID, t.FamilyID}
		if seen[key] {
			continue
		}
		seen[key] = true
		families[t.UserID] = append(families[t.UserID], t.FamilyID)
	}
	return families, nil
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

func (s *fakeStore) ListRoles(_ context.Context) ([]domain.Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("ListRoles"); err != nil {
		return nil, err
	}

	out := make([]domain.Role, 0, len(s.roles))
	for _, r := range s.roles {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *fakeStore) RoleNamesForUser(_ context.Context, userID uuid.UUID) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("RoleNamesForUser"); err != nil {
		return nil, err
	}

	var names []string
	for roleID := range s.userRoles[userID] {
		names = append(names, s.roles[roleID].Name)
	}
	sort.Strings(names)
	return names, nil
}

func (s *fakeStore) AssignRole(_ context.Context, userID, roleID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("AssignRole"); err != nil {
		return err
	}
	if _, ok := s.roles[roleID]; !ok {
		return domain.ErrRoleNotFound
	}
	if _, ok := s.users[userID]; !ok {
		return domain.ErrUserNotFound
	}

	if s.userRoles[userID] == nil {
		s.userRoles[userID] = make(map[uuid.UUID]bool)
	}
	s.userRoles[userID][roleID] = true
	return nil
}

func (s *fakeStore) RevokeRole(_ context.Context, userID, roleID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("RevokeRole"); err != nil {
		return err
	}
	if !s.userRoles[userID][roleID] {
		return domain.ErrRoleNotFound
	}

	delete(s.userRoles[userID], roleID)
	return nil
}

func (s *fakeStore) UserHasPermission(_ context.Context, userID uuid.UUID, namespace, action string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("UserHasPermission"); err != nil {
		return false, err
	}
	if s.users[userID].Suspended() {
		return false, nil
	}

	for roleID := range s.userRoles[userID] {
		granted := s.rolePermissions[roleID]
		if granted[namespace+":"+action] || granted[namespace+":manage"] {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeStore) SearchUsers(_ context.Context, query string, excludeUserID uuid.UUID, limit int, after *domain.UserSearchCursor) ([]domain.UserSearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("SearchUsers"); err != nil {
		return nil, err
	}

	lowered := strings.ToLower(query)
	username := strings.TrimLeft(lowered, "@")
	rank := func(u domain.User) int {
		name, nickname := strings.ToLower(u.DisplayName), strings.ToLower(u.Nickname)
		switch {
		case name == lowered || u.Username == username:
			return 0
		case strings.HasPrefix(name, lowered) || strings.HasPrefix(nickname, lowered) ||
			(username != "" && strings.HasPrefix(u.Username, username)):
			return 1
		case strings.Contains(name, lowered) || strings.Contains(nickname, lowered) ||
			(username != "" && strings.Contains(u.Username, username)):
			return 2
		default:
			return -1
		}
	}

	compare := func(a, b domain.UserSearchCursor) int {
		if a.Rank != b.Rank {
			return a.Rank - b.Rank
		}
		if n := strings.Compare(a.Name, b.Name); n != 0 {
			return n
		}
		if n := strings.Compare(a.Username, b.Username); n != 0 {
			return n
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	}
	var out []domain.UserSearchResult
	for _, u := range s.users {
		cursor := domain.UserSearchCursor{Rank: rank(u), Name: strings.ToLower(u.DisplayName), Username: u.Username, ID: u.ID}
		if u.ID != excludeUserID && cursor.Rank >= 0 && (after == nil || compare(cursor, *after) > 0) {
			out = append(out, domain.UserSearchResult{User: u, Cursor: cursor})
		}
	}
	sort.Slice(out, func(i, j int) bool { return compare(out[i].Cursor, out[j].Cursor) < 0 })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *fakeStore) UsersByIDs(_ context.Context, ids []uuid.UUID) ([]domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("UsersByIDs"); err != nil {
		return nil, err
	}

	var out []domain.User
	for _, id := range ids {
		if u, ok := s.users[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
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

func (s *fakeStore) RecordSecurityEvent(ctx context.Context, e domain.SecurityEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.fail("RecordSecurityEvent"); err != nil {
		return err
	}
	s.securityEvents = append(s.securityEvents, e)
	return nil
}

func (s *fakeStore) recordedSecurityEvents() []domain.SecurityEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]domain.SecurityEvent(nil), s.securityEvents...)
}
