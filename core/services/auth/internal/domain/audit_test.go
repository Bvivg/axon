package domain_test

import (
	"context"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func TestEmailSHA256HashesTheNormalizedAddress(t *testing.T) {
	const want = "4d376d37aa81d5437fc0400002613ac72c5b0fabcff59696d9de587e2b596a53"

	for _, email := range []string{
		"ada@axon.test",
		"Ada@Axon.Test",
		"  ada@axon.test\t",
		"ADA@AXON.TEST ",
	} {
		t.Run(email, func(t *testing.T) {
			got := domain.EmailSHA256(email)
			if got != want {
				t.Fatalf("EmailSHA256(%q) = %s, want %s", email, got, want)
			}
			if strings.Contains(got, "ada") || strings.Contains(got, "axon") {
				t.Fatalf("hash %s leaks part of the address", got)
			}
		})
	}
}

func TestEmailSHA256SeparatesDifferentAddresses(t *testing.T) {
	if domain.EmailSHA256("ada@axon.test") == domain.EmailSHA256("grace@axon.test") {
		t.Fatal("two addresses share a hash")
	}
}

func TestDeviceTravelsThroughTheContext(t *testing.T) {
	for name, tt := range map[string]struct {
		ctx  context.Context
		want domain.Device
	}{
		"stored": {
			ctx:  domain.ContextWithDevice(context.Background(), domain.Device{UserAgent: "Firefox/140", IP: "203.0.113.7"}),
			want: domain.Device{UserAgent: "Firefox/140", IP: "203.0.113.7"},
		},
		"absent": {
			ctx:  context.Background(),
			want: domain.Device{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := domain.DeviceFromContext(tt.ctx); got != tt.want {
				t.Fatalf("DeviceFromContext = %+v, want %+v", got, tt.want)
			}
		})
	}
}
