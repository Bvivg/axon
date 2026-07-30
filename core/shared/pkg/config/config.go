// Package config loads service configuration from the environment.
//
// Two rules shape the API. First, fail fast: a service with missing or
// malformed configuration must refuse to start rather than discover the
// problem on the first request. Second, report everything at once: the Loader
// accumulates every error and returns them joined, so a fresh deployment does
// not need five restarts to learn about five missing variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Loader reads values from an environment and collects the errors it hits
// along the way. A Loader is single-use and not safe for concurrent use.
type Loader struct {
	lookup func(string) (string, bool)
	errs   []error
}

// NewLoader returns a Loader reading from the process environment.
func NewLoader() *Loader {
	return &Loader{lookup: os.LookupEnv}
}

// NewLoaderFromMap returns a Loader reading from env. Tests use it to avoid
// mutating the process environment.
func NewLoaderFromMap(env map[string]string) *Loader {
	return &Loader{
		lookup: func(key string) (string, bool) {
			v, ok := env[key]
			return v, ok
		},
	}
}

// Err returns every error the Loader accumulated, joined, or nil when the
// configuration is complete and well-formed. Callers must check it before
// using any loaded value.
func (l *Loader) Err() error {
	if len(l.errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration: %w", errors.Join(l.errs...))
}

func (l *Loader) fail(key string, err error) {
	l.errs = append(l.errs, fmt.Errorf("%s: %w", key, err))
}

// ErrMissing is reported for a required variable that is unset or empty.
var ErrMissing = errors.New("required but not set")

// raw returns the trimmed value of key. present is false when the variable is
// unset or holds only whitespace — an empty string in the environment is
// treated as absent, since that is how docker-compose renders an unset value.
func (l *Loader) raw(key string) (value string, present bool) {
	v, ok := l.lookup(key)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

// String returns a required string value.
func (l *Loader) String(key string) string {
	v, ok := l.raw(key)
	if !ok {
		l.fail(key, ErrMissing)
		return ""
	}
	return v
}

// StringDefault returns key's value, or def when it is not set.
func (l *Loader) StringDefault(key, def string) string {
	if v, ok := l.raw(key); ok {
		return v
	}
	return def
}

// Secret returns a required secret value.
func (l *Loader) Secret(key string) Secret {
	v, ok := l.raw(key)
	if !ok {
		l.fail(key, ErrMissing)
		return Secret{}
	}
	return NewSecret(v)
}

// SecretDefault returns key's value as a Secret, or def when it is not set.
// Only for values that are genuinely optional — never to paper over a missing
// production credential with a development placeholder.
func (l *Loader) SecretDefault(key, def string) Secret {
	if v, ok := l.raw(key); ok {
		return NewSecret(v)
	}
	return NewSecret(def)
}

// Int returns a required integer value.
func (l *Loader) Int(key string) int {
	v, ok := l.raw(key)
	if !ok {
		l.fail(key, ErrMissing)
		return 0
	}
	return l.parseInt(key, v)
}

// IntDefault returns key's value as an integer, or def when it is not set. A
// malformed value is still an error: a typo must not silently become def.
func (l *Loader) IntDefault(key string, def int) int {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	return l.parseInt(key, v)
}

func (l *Loader) parseInt(key, v string) int {
	n, err := strconv.Atoi(v)
	if err != nil {
		l.fail(key, fmt.Errorf("not an integer: %q", v))
		return 0
	}
	return n
}

// Bool returns key's value as a bool, or def when it is not set. Accepts the
// spellings strconv.ParseBool does (1/t/T/TRUE/true/True and the false forms).
func (l *Loader) Bool(key string, def bool) bool {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail(key, fmt.Errorf("not a boolean: %q", v))
		return def
	}
	return b
}

// Duration returns a required duration value in Go syntax, e.g. "15m".
func (l *Loader) Duration(key string) time.Duration {
	v, ok := l.raw(key)
	if !ok {
		l.fail(key, ErrMissing)
		return 0
	}
	return l.parseDuration(key, v)
}

// DurationDefault returns key's value as a duration, or def when it is not set.
func (l *Loader) DurationDefault(key string, def time.Duration) time.Duration {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	return l.parseDuration(key, v)
}

func (l *Loader) parseDuration(key, v string) time.Duration {
	d, err := time.ParseDuration(v)
	if err != nil {
		l.fail(key, fmt.Errorf("not a duration: %q (want Go syntax such as 15m)", v))
		return 0
	}
	return d
}

// StringSlice returns key's value split on commas with entries trimmed, or def
// when it is not set. Used for allow-lists such as CORS origins.
func (l *Loader) StringSlice(key string, def []string) []string {
	v, ok := l.raw(key)
	if !ok {
		return def
	}

	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		l.fail(key, fmt.Errorf("no values in %q", v))
		return def
	}
	return out
}

// OneOf returns key's value constrained to allowed, or def when it is not set.
func (l *Loader) OneOf(key, def string, allowed ...string) string {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	l.fail(key, fmt.Errorf("got %q, want one of %s", v, strings.Join(allowed, ", ")))
	return def
}
