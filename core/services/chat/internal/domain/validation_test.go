package domain_test

import (
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func TestValidateRoomName(t *testing.T) {
	long := strings.Repeat("к", domain.MaxRoomNameLength)

	for name, tc := range map[string]struct {
		in      string
		want    string
		wantErr bool
	}{
		"ordinary":            {in: "general", want: "general"},
		"trimmed":             {in: "  general  ", want: "general"},
		"empty":               {in: "", wantErr: true},
		"only whitespace":     {in: "   \t\n ", wantErr: true},
		"at the limit":        {in: long, want: long},
		"over the limit":      {in: long + "к", wantErr: true},
		"counted in runes":    {in: strings.Repeat("к", 100), want: strings.Repeat("к", 100)},
		"invalid UTF-8 bytes": {in: "room\xff", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := domain.ValidateRoomName(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateRoomName(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateRoomName(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ValidateRoomName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The bound here is also a CHECK constraint on the messages table. If the two
// ever disagree, this is the side that should refuse first — a row the database
// rejects surfaces as an internal error rather than as a message somebody can
// shorten.
func TestValidateMessageBody(t *testing.T) {
	long := strings.Repeat("м", domain.MaxMessageLength)

	for name, tc := range map[string]struct {
		in      string
		want    string
		wantErr bool
	}{
		"ordinary":            {in: "hello", want: "hello"},
		"trailing whitespace": {in: "hello   \n", want: "hello"},
		"empty":               {in: "", wantErr: true},
		"only whitespace":     {in: "  \t ", wantErr: true},
		"at the limit":        {in: long, want: long},
		"over the limit":      {in: long + "м", wantErr: true},
		"invalid UTF-8 bytes": {in: "hi\xff", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := domain.ValidateMessageBody(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateMessageBody(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateMessageBody(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ValidateMessageBody(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// An absent client id is allowed: it costs the sender the delivery echo and the
// protection against a resend landing twice, and nothing else.
func TestValidateClientID(t *testing.T) {
	for name, tc := range map[string]struct {
		in      string
		want    string
		wantErr bool
	}{
		"a uuid":         {in: "5a8b1f5e-1c2d-4a3b-8c9d-0e1f2a3b4c5d", want: "5a8b1f5e-1c2d-4a3b-8c9d-0e1f2a3b4c5d"},
		"absent":         {in: ""},
		"whitespace":     {in: "   "},
		"over the limit": {in: strings.Repeat("x", domain.MaxClientIDLength+1), wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := domain.ValidateClientID(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateClientID(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateClientID(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ValidateClientID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The name comes from auth, not from the caller, so nothing about it may fail a
// request. Every unusable value has to resolve to something storable.
func TestValidateDisplayNameNeverFails(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want string
	}{
		"ordinary":       {in: "Ada Lovelace", want: "Ada Lovelace"},
		"trimmed":        {in: "  Ada  ", want: "Ada"},
		"absent":         {in: ""},
		"invalid UTF-8":  {in: "Ada\xff"},
		"absurdly long":  {in: strings.Repeat("и", domain.MaxRoomNameLength+40), want: strings.Repeat("и", domain.MaxRoomNameLength)},
		"exactly at cap": {in: strings.Repeat("и", domain.MaxRoomNameLength), want: strings.Repeat("и", domain.MaxRoomNameLength)},
	} {
		t.Run(name, func(t *testing.T) {
			if got := domain.ValidateDisplayName(tc.in); got != tc.want {
				t.Errorf("ValidateDisplayName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidatePage(t *testing.T) {
	for name, tc := range map[string]struct {
		in      domain.Page
		want    domain.Page
		wantErr bool
	}{
		"no limit given": {
			in:   domain.Page{},
			want: domain.Page{Limit: domain.DefaultPageSize},
		},
		"limit clamped": {
			in:   domain.Page{Limit: domain.MaxPageSize + 1000},
			want: domain.Page{Limit: domain.MaxPageSize},
		},
		"paging back": {
			in:   domain.Page{Limit: 10, BeforeSeq: 40},
			want: domain.Page{Limit: 10, BeforeSeq: 40},
		},
		"catching up": {
			in:   domain.Page{Limit: 10, AfterSeq: 40},
			want: domain.Page{Limit: 10, AfterSeq: 40},
		},
		// Bounded in both directions describes no page anyone could return, so
		// it is refused rather than silently resolved one way.
		"both cursors":    {in: domain.Page{BeforeSeq: 10, AfterSeq: 2}, wantErr: true},
		"negative cursor": {in: domain.Page{AfterSeq: -1}, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := domain.ValidatePage(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidatePage(%+v) = %+v, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatePage(%+v): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ValidatePage(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// The most recent page is the first step of paging backwards, so it reads the
// same direction as an explicit BeforeSeq.
func TestPageDirection(t *testing.T) {
	for name, tc := range map[string]struct {
		page domain.Page
		want bool
	}{
		"the most recent page": {page: domain.Page{Limit: 10}, want: true},
		"paging back":          {page: domain.Page{BeforeSeq: 5}, want: true},
		"catching up":          {page: domain.Page{AfterSeq: 5}, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := tc.page.Backward(); got != tc.want {
				t.Errorf("Backward() = %v, want %v", got, tc.want)
			}
		})
	}
}
