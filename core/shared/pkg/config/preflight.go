package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

var ErrUnsafeInProduction = errors.New("refused in production")

var knownDefaultSecrets = map[string]struct{}{
	"axon":                        {},
	"axon12345":                   {},
	"axon-edge":                   {},
	"auth":                        {},
	"chat":                        {},
	"game":                        {},
	"calling":                     {},
	"rating":                      {},
	"analysis":                    {},
	"tournament":                  {},
	"debezium":                    {},
	"replicator":                  {},
	"axon-step-ca":                {},
	"unused-after-drill":          {},
	"axon-avatars-dev-secret":     {},
	"axon-attachments-dev-secret": {},
	"axon-dev-game-owner-key-not-for-production": {},
}

var secretKeys = []string{
	"POSTGRES_PASSWORD",
	"AUTH_DB_PASSWORD",
	"CHAT_DB_PASSWORD",
	"GAME_DB_PASSWORD",
	"CALLING_DB_PASSWORD",
	"RATING_DB_PASSWORD",
	"ANALYSIS_DB_PASSWORD",
	"TOURNAMENT_DB_PASSWORD",
	"DEBEZIUM_DB_PASSWORD",
	"POSTGRES_REPLICATION_PASSWORD",
	"REDIS_PASSWORD",
	"EDGE_REDIS_PASSWORD",
	"RATE_LIMIT_REDIS_PASSWORD",
	"MINIO_ROOT_PASSWORD",
	"AVATARS_SECRET_KEY",
	"ATTACHMENTS_SECRET_KEY",
	"GAME_INTERNAL_KEY",
	"STEP_CA_PASSWORD",
}

var databaseURLKeys = []string{
	"POSTGRES_DSN",
	"AUTH_POSTGRES_DSN",
	"CHAT_POSTGRES_DSN",
	"GAME_POSTGRES_DSN",
	"CALLING_POSTGRES_DSN",
	"RATING_POSTGRES_DSN",
	"ANALYSIS_POSTGRES_DSN",
	"TOURNAMENT_POSTGRES_DSN",
	"GAME_LEASE_DATABASE_URL",
	"GAME_READ_DATABASE_URL",
	"GAME_LISTEN_DATABASE_URL",
}

var developmentSwitches = []string{
	"OAUTH_FAKE_ENABLED",
	"GAME_TEST_HOOKS",
	"PPROF_ENABLED",
}

func IsKnownDefaultSecret(value string) bool {
	_, known := knownDefaultSecrets[value]
	return known
}

func productionPreflight(l *Loader) {
	for _, key := range secretKeys {
		if v, ok := l.raw(key); ok && IsKnownDefaultSecret(v) {
			l.fail(key, fmt.Errorf("%w: a default secret from the repository", ErrUnsafeInProduction))
		}
	}
	for _, key := range databaseURLKeys {
		if v, ok := l.raw(key); ok && hasDefaultPassword(v) {
			l.fail(key, fmt.Errorf("%w: the URL carries a default password from the repository", ErrUnsafeInProduction))
		}
	}
	for _, key := range developmentSwitches {
		if enabled, ok := l.parsedBool(key); ok && enabled {
			l.fail(key, fmt.Errorf("%w: a development-only switch", ErrUnsafeInProduction))
		}
	}
	if secure, ok := l.parsedBool("REFRESH_COOKIE_SECURE"); ok && !secure {
		l.fail("REFRESH_COOKIE_SECURE", fmt.Errorf("%w: the refresh cookie must be Secure", ErrUnsafeInProduction))
	}
}

func (b Base) RefuseDefaultSecret(l *Loader, key, value string) {
	if b.Environment.IsProduction() && IsKnownDefaultSecret(value) {
		l.fail(key, fmt.Errorf("%w: a default secret from the repository", ErrUnsafeInProduction))
	}
}

func (l *Loader) parsedBool(key string) (value, ok bool) {
	raw, present := l.raw(key)
	if !present {
		return false, false
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return parsed, true
}

func hasDefaultPassword(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return false
	}
	password, set := u.User.Password()
	return set && IsKnownDefaultSecret(password)
}
