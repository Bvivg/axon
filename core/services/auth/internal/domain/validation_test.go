package domain_test

import (
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain", in: "bob@example.com", want: "bob@example.com"},
		{name: "uppercase is normalized", in: "Bob@Example.COM", want: "bob@example.com"},
		{name: "surrounding space is trimmed", in: "  bob@example.com \t", want: "bob@example.com"},
		{name: "plus addressing is allowed", in: "bob+axon@example.com", want: "bob+axon@example.com"},
		{name: "empty", in: "", wantErr: true},
		{name: "blank", in: "   ", wantErr: true},
		{name: "no at sign", in: "bob.example.com", wantErr: true},
		{name: "no domain", in: "bob@", wantErr: true},
		{name: "display name form is refused", in: "Bob <bob@example.com>", wantErr: true},
		{name: "two addresses", in: "bob@example.com, eve@example.com", wantErr: true},
		{name: "too long", in: strings.Repeat("a", 250) + "@example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ValidateEmail(tt.in)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateEmail(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ValidateEmail(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateEmailReportsTheField(t *testing.T) {
	_, err := domain.ValidateEmail("nope")

	v, ok := domain.AsValidationError(err)
	if !ok {
		t.Fatalf("error is not a ValidationError: %v", err)
	}
	if v.Field != "email" {
		t.Errorf("Field = %q, want email", v.Field)
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{name: "at the minimum", in: strings.Repeat("a", domain.MinPasswordLength)},
		{name: "typical passphrase", in: "correct horse battery staple"},
		{name: "at the maximum", in: strings.Repeat("a", domain.MaxPasswordLength)},
		{name: "empty", in: "", wantErr: true},
		{name: "one below the minimum", in: strings.Repeat("a", domain.MinPasswordLength-1), wantErr: true},
		{name: "one above the maximum", in: strings.Repeat("a", domain.MaxPasswordLength+1), wantErr: true},
		{name: "invalid utf-8", in: "pass\xffword", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidatePassword(tt.in)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePassword(len=%d) error = %v, wantErr %v", len(tt.in), err, tt.wantErr)
			}
		})
	}
}

func TestPasswordLimitIsMeasuredInBytes(t *testing.T) {

	within := strings.Repeat("п", domain.MaxPasswordLength/2)
	if err := domain.ValidatePassword(within); err != nil {
		t.Fatalf("password of %d bytes was refused: %v", len(within), err)
	}

	over := strings.Repeat("п", domain.MaxPasswordLength/2+1)
	if err := domain.ValidatePassword(over); err == nil {
		t.Fatalf("password of %d bytes was accepted", len(over))
	}
}

func TestValidateDisplayName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty is allowed", in: "", want: ""},
		{name: "blank collapses to empty", in: "   ", want: ""},
		{name: "trimmed", in: "  Bob  ", want: "Bob"},
		{name: "at the limit", in: strings.Repeat("б", domain.MaxDisplayNameLength), want: strings.Repeat("б", domain.MaxDisplayNameLength)},
		{name: "over the limit", in: strings.Repeat("б", domain.MaxDisplayNameLength+1), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.ValidateDisplayName(tt.in)

			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDisplayNameLimitIsMeasuredInRunes(t *testing.T) {
	name := strings.Repeat("б", domain.MaxDisplayNameLength)

	if _, err := domain.ValidateDisplayName(name); err != nil {
		t.Fatalf("%d-rune name (%d bytes) was refused: %v", domain.MaxDisplayNameLength, len(name), err)
	}
}

func TestProviderValid(t *testing.T) {
	valid := []domain.Provider{
		domain.ProviderGoogle,
		domain.ProviderGitHub,
		domain.ProviderApple,
		domain.ProviderFake,
	}
	for _, p := range valid {
		if !p.Valid() {
			t.Errorf("Provider(%q).Valid() = false, want true", p)
		}
	}

	for _, p := range []domain.Provider{"", "facebook", "GOOGLE"} {
		if p.Valid() {
			t.Errorf("Provider(%q).Valid() = true, want false", p)
		}
	}
}
