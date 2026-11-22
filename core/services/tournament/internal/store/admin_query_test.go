package store

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTournamentSchemaCompilesEveryField(t *testing.T) {
	id := uuid.MustParse("9a4c6f0e-31d5-4f6b-9a55-0b8b7f1f3c11")
	day := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		field  string
		filter string
		sql    string
		args   []any
	}{
		{"id", "id = " + id.String(), "t.id = ?", []any{id}},
		{"name", `name:"cup"`, `t.name ILIKE ? ESCAPE '\'`, []any{"%cup%"}},
		{"game", `game = "chess"`, "t.game = ?", []any{"chess"}},
		{"status", "status = registration", "t.status = 'registration'", nil},
		{"status", "status != cancelled", "NOT (t.status = 'cancelled')", nil},
		{"format", "format = single_elimination", "t.format = 'single_elimination'", nil},
		{"format", "format = swiss", "t.format = 'swiss'", nil},
		{"players", "players >= 4", registeredPlayers + " >= ?", []any{int64(4)}},
		{"capacity", "capacity = 8", "t.capacity = ?", []any{int64(8)}},
		{"created", "created < 2026-11-20", "t.created_at < ?", []any{day}},
		{"started", "started >= 2026-11-20", "t.started_at >= ?", []any{day}},
		{"creator", "creator = " + id.String(), "t.creator_id = ?", []any{id}},
	}
	covered := map[string]bool{}
	for _, tt := range cases {
		q, err := tournamentSchema.Compile(tt.filter, "")
		if err != nil {
			t.Errorf("Compile(%q): %v", tt.filter, err)
			continue
		}
		if q.Where == nil || q.Where.SQL != tt.sql || !reflect.DeepEqual(q.Where.Args, tt.args) {
			t.Errorf("Compile(%q) = %+v, want %q %#v", tt.filter, q.Where, tt.sql, tt.args)
		}
		covered[tt.field] = true
	}
	for _, f := range tournamentSchema.Fields {
		if !covered[f.Name] {
			t.Errorf("field %q has no filter case", f.Name)
		}
		if !f.Sortable {
			continue
		}
		q, err := tournamentSchema.Compile("", f.Name+" desc")
		if err != nil {
			t.Errorf("order_by %q: %v", f.Name, err)
			continue
		}
		if !strings.HasPrefix(q.Order, f.Column+" DESC NULLS LAST, ") {
			t.Errorf("order_by %q = %q", f.Name, q.Order)
		}
	}
	for _, status := range []string{"registration", "seeding", "running", "finished", "cancelled"} {
		if _, err := tournamentSchema.Compile("status = "+status, ""); err != nil {
			t.Errorf("status %q: %v", status, err)
		}
	}
	if q, err := tournamentSchema.Compile("", ""); err != nil || q.Order != "t.created_at DESC, t.id DESC" {
		t.Errorf("default order = %q, %v", q.Order, err)
	}
}
