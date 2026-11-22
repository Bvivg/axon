package session

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"
)

type schemaCase struct {
	field  string
	filter string
	sql    string
	args   []any
}

func checkSchema(t *testing.T, schema *adminquery.Schema, cases []schemaCase) {
	t.Helper()
	covered := map[string]bool{}
	for _, tt := range cases {
		q, err := schema.Compile(tt.filter, "")
		if err != nil {
			t.Errorf("Compile(%q): %v", tt.filter, err)
			continue
		}
		if q.Where == nil || q.Where.SQL != tt.sql || !reflect.DeepEqual(q.Where.Args, tt.args) {
			t.Errorf("Compile(%q) = %+v, want %q %#v", tt.filter, q.Where, tt.sql, tt.args)
		}
		covered[tt.field] = true
	}
	for _, f := range schema.Fields {
		if !covered[f.Name] {
			t.Errorf("field %q has no filter case", f.Name)
		}
		if !f.Sortable {
			continue
		}
		q, err := schema.Compile("", f.Name+" desc")
		if err != nil {
			t.Errorf("order_by %q: %v", f.Name, err)
			continue
		}
		if !strings.HasPrefix(q.Order, f.Column+" DESC NULLS LAST, ") {
			t.Errorf("order_by %q = %q", f.Name, q.Order)
		}
	}
}

func TestSessionSchemaCompilesEveryField(t *testing.T) {
	id := uuid.MustParse("9a4c6f0e-31d5-4f6b-9a55-0b8b7f1f3c11")
	day := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	checkSchema(t, sessionSchema, []schemaCase{
		{"id", "id = " + id.String(), "id = ?", []any{id}},
		{"game", `game = "chess"`, "game = ?", []any{"chess"}},
		{"status", "status = live", liveSession, nil},
		{"status", "status = waiting", waitingSession, nil},
		{"status", "status != finished", "NOT (" + finishedSession + ")", nil},
		{"status", "status = won", "opponent_id IS NOT NULL AND state->>'status' = 'win'", nil},
		{"status", "status = draw", "opponent_id IS NOT NULL AND state->>'status' = 'draw'", nil},
		{"player", "player = " + id.String(), "(creator_id = ? OR opponent_id IS NOT DISTINCT FROM ?)", []any{id, id}},
		{"player", "player != " + id.String(), "(creator_id <> ? AND opponent_id IS DISTINCT FROM ?)", []any{id, id}},
		{"tournament", "tournament = " + id.String(), "tournament_id = ?", []any{id}},
		{"rated", "rated = true", "rated = ?", []any{true}},
		{"moves", "moves >= 40", "COALESCE((state->>'ply')::integer, 0) >= ?", []any{int64(40)}},
		{"updated", "updated < 2026-11-20", "updated_at < ?", []any{day}},
	})
	if _, err := sessionSchema.Compile("player = nobody", ""); err == nil {
		t.Error("a malformed player id compiled")
	}
	if _, err := sessionSchema.Compile("player > "+id.String(), ""); err == nil {
		t.Error("a range on player compiled")
	}
}

func TestQueueSchemaCompilesEveryField(t *testing.T) {
	id := uuid.MustParse("9a4c6f0e-31d5-4f6b-9a55-0b8b7f1f3c11")
	day := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	checkSchema(t, queueSchema, []schemaCase{
		{"user", "user = " + id.String(), "user_id = ?", []any{id}},
		{"game", `game:"quo"`, `game ILIKE ? ESCAPE '\'`, []any{"%quo%"}},
		{"rating", "rating > 1500", "rating > ?", []any{int64(1500)}},
		{"joined", "joined >= 2026-11-20", "joined_at >= ?", []any{day}},
		{"matched", "matched = true", "(session_id IS NOT NULL) = ?", []any{true}},
	})
}
