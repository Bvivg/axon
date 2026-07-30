package config

import "log/slog"

// redacted is what a Secret renders as everywhere except Reveal.
const redacted = "[REDACTED]"

// Secret wraps a configuration value that must never reach a log line, an
// error message or a stack dump. It satisfies fmt.Stringer, slog.LogValuer and
// the json/text marshalers, so every accidental path out prints the redaction
// placeholder. The real value is only available through Reveal, which is
// grep-able during review.
type Secret struct {
	value string
}

// NewSecret wraps value.
func NewSecret(value string) Secret {
	return Secret{value: value}
}

// Reveal returns the underlying value. Call it at the point of use — passing a
// DSN to a driver, an OAuth secret to a provider — and never store the result.
func (s Secret) Reveal() string {
	return s.value
}

// IsZero reports whether the secret holds no value. Useful for validation
// without revealing anything.
func (s Secret) IsZero() bool {
	return s.value == ""
}

// String implements fmt.Stringer and hides the value from %s and %v.
func (s Secret) String() string {
	return redacted
}

// GoString hides the value from %#v as well.
func (s Secret) GoString() string {
	return redacted
}

// LogValue implements slog.LogValuer so structured logs never carry the value.
func (s Secret) LogValue() slog.Value {
	return slog.StringValue(redacted)
}

// MarshalJSON keeps secrets out of any struct that gets serialised, e.g. a
// config dump on a debug endpoint.
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redacted + `"`), nil
}

// MarshalText covers encoders that prefer TextMarshaler.
func (s Secret) MarshalText() ([]byte, error) {
	return []byte(redacted), nil
}
