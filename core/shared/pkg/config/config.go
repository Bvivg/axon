package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Loader struct {
	lookup func(string) (string, bool)
	errs   []error
}

func NewLoader() *Loader {
	return &Loader{lookup: os.LookupEnv}
}

func NewLoaderFromMap(env map[string]string) *Loader {
	return &Loader{
		lookup: func(key string) (string, bool) {
			v, ok := env[key]
			return v, ok
		},
	}
}

func (l *Loader) Err() error {
	if len(l.errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid configuration: %w", errors.Join(l.errs...))
}

func (l *Loader) fail(key string, err error) {
	l.errs = append(l.errs, fmt.Errorf("%s: %w", key, err))
}

func (l *Loader) Fail(key string, err error) {
	l.fail(key, err)
}

var ErrMissing = errors.New("required but not set")

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

func (l *Loader) String(key string) string {
	v, ok := l.raw(key)
	if !ok {
		l.fail(key, ErrMissing)
		return ""
	}
	return v
}

func (l *Loader) StringDefault(key, def string) string {
	if v, ok := l.raw(key); ok {
		return v
	}
	return def
}

func (l *Loader) Secret(key string) Secret {
	v, ok := l.raw(key)
	if !ok {
		l.fail(key, ErrMissing)
		return Secret{}
	}
	return NewSecret(v)
}

func (l *Loader) SecretDefault(key, def string) Secret {
	if v, ok := l.raw(key); ok {
		return NewSecret(v)
	}
	return NewSecret(def)
}

func (l *Loader) Int(key string) int {
	v, ok := l.raw(key)
	if !ok {
		l.fail(key, ErrMissing)
		return 0
	}
	return l.parseInt(key, v)
}

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

func (l *Loader) Duration(key string) time.Duration {
	v, ok := l.raw(key)
	if !ok {
		l.fail(key, ErrMissing)
		return 0
	}
	return l.parseDuration(key, v)
}

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
