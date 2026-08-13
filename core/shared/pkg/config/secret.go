package config

import "log/slog"

const redacted = "[REDACTED]"

type Secret struct {
	value string
}

func NewSecret(value string) Secret {
	return Secret{value: value}
}

func (s Secret) Reveal() string {
	return s.value
}

func (s Secret) IsZero() bool {
	return s.value == ""
}

func (s Secret) String() string {
	return redacted
}

func (s Secret) GoString() string {
	return redacted
}

func (s Secret) LogValue() slog.Value {
	return slog.StringValue(redacted)
}

func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redacted + `"`), nil
}

func (s Secret) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}
